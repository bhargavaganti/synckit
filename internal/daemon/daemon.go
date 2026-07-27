// Package daemon serves and receives bundles over the tailnet. It is the peer
// side of the Tailscale transport: `synckit serve` runs it, binding to this
// machine's tailnet IP so only tailnet peers can reach it.
package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bhargav/synckit/internal/bundle"
)

// Config configures the daemon.
type Config struct {
	SpoolDir    string      // where received bundles are stored
	BindIP      string      // tailnet IP to bind (empty = all interfaces)
	Port        int         // listen port
	OnReceive   func(path string) // optional hook (e.g. auto-restore); may be nil
	Logger      *log.Logger
}

// Server holds a running daemon.
type Server struct {
	cfg Config
	srv *http.Server
}

// New builds a daemon. It does not start listening until Serve.
func New(cfg Config) (*Server, error) {
	if cfg.SpoolDir == "" {
		return nil, fmt.Errorf("daemon: SpoolDir required")
	}
	if err := os.MkdirAll(cfg.SpoolDir, 0o755); err != nil {
		return nil, err
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "synckit-daemon ", log.LstdFlags)
	}
	s := &Server{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/bundles", s.handleList)       // GET  → list
	mux.HandleFunc("/bundles/", s.handleBundle)    // GET/PUT single by name
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	addr := net.JoinHostPort(cfg.BindIP, fmt.Sprintf("%d", cfg.Port))
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           s.tailnetOnly(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

// Serve blocks, listening for peers.
func (s *Server) Serve() error {
	s.cfg.Logger.Printf("listening on %s, spool=%s", s.srv.Addr, s.cfg.SpoolDir)
	return s.srv.ListenAndServe()
}

// tailnetOnly rejects requests whose source IP is not a tailnet (100.64.0.0/10)
// address, as defense-in-depth if the daemon is ever bound beyond the tailnet.
func (s *Server) tailnetOnly(next http.Handler) http.Handler {
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if ip != nil && (ip.IsLoopback() || cgnat.Contains(ip)) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "forbidden: tailnet peers only", http.StatusForbidden)
	})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries, _ := os.ReadDir(s.cfg.SpoolDir)
	type item struct {
		Name string          `json:"name"`
		Meta bundle.Metadata `json:"meta"`
	}
	var out []item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".zip") {
			continue
		}
		p := filepath.Join(s.cfg.SpoolDir, e.Name())
		meta, err := bundle.ReadMetadata(p)
		if err != nil {
			continue
		}
		out = append(out, item{Name: e.Name(), Meta: meta})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleBundle(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/bundles/")
	// Guard against path traversal: only a bare filename is allowed.
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		http.Error(w, "bad bundle name", http.StatusBadRequest)
		return
	}
	dst := filepath.Join(s.cfg.SpoolDir, name)

	switch r.Method {
	case http.MethodGet:
		http.ServeFile(w, r, dst)
	case http.MethodPut, http.MethodPost:
		if err := s.receive(dst, r.Body); err != nil {
			s.cfg.Logger.Printf("receive %s: %v", name, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.cfg.Logger.Printf("received %s", name)
		if s.cfg.OnReceive != nil {
			go s.cfg.OnReceive(dst)
		}
		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// receive streams an upload to a temp file, validates it parses as a bundle,
// then atomically renames it into the spool so partial/garbage uploads never
// appear as valid bundles.
func (s *Server) receive(dst string, body io.Reader) error {
	tmp, err := os.CreateTemp(s.cfg.SpoolDir, ".incoming-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := bundle.ReadMetadata(tmpName); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("not a valid bundle: %w", err)
	}
	return os.Rename(tmpName, dst)
}
