package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/bhargav/synckit/internal/bundle"
)

// DefaultPort is the port the synckit daemon listens on over the tailnet.
const DefaultPort = 8737

// Tailscale is the network transport: it talks HTTP to a peer's `synckit serve`
// daemon over the tailnet (100.x addresses or MagicDNS names). Reachability and
// auth are handled by Tailscale itself — inside a tailnet, peers are already
// mutually authenticated, so the daemon trusts tailnet-sourced requests.
type Tailscale struct {
	Peer string // tailnet host: "100.101.102.103" or "laptop.tail-scale.ts.net"
	Port int
	HTTP *http.Client
}

func NewTailscale(peer string, port int) *Tailscale {
	if port == 0 {
		port = DefaultPort
	}
	return &Tailscale{
		Peer: peer,
		Port: port,
		// No overall request timeout — a multi-GB bundle can legitimately take
		// many minutes to stream. Bound the connection setup and header wait
		// instead, so a dead peer still fails fast but a slow-but-alive transfer
		// is allowed to finish.
		HTTP: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				ResponseHeaderTimeout: 60 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}
}

func (t *Tailscale) Name() string { return "tailscale" }

func (t *Tailscale) base() string {
	return fmt.Sprintf("http://%s:%d", t.Peer, t.Port)
}

// Reachable reports whether a synckit daemon answers on the peer within the
// given timeout. Used by discovery to tell "machine online" from "machine
// online AND serving synckit".
func (t *Tailscale) Reachable(timeout time.Duration) bool {
	c := &http.Client{Timeout: timeout}
	resp, err := c.Get(t.base() + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

func (t *Tailscale) Put(srcPath string) (Ref, error) {
	meta, err := bundle.ReadMetadata(srcPath)
	if err != nil {
		return Ref{}, fmt.Errorf("read bundle metadata: %w", err)
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return Ref{}, err
	}
	defer f.Close()

	u := t.base() + "/bundles/" + url.PathEscape(filepath.Base(srcPath))
	req, err := http.NewRequest(http.MethodPut, u, f)
	if err != nil {
		return Ref{}, err
	}
	req.Header.Set("Content-Type", "application/zip")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return Ref{}, fmt.Errorf("push to %s: %w", t.Peer, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Ref{}, fmt.Errorf("push rejected (%s): %s", resp.Status, body)
	}
	return Ref{ID: meta.ID, Location: t.Peer + ":" + filepath.Base(srcPath), Meta: meta}, nil
}

func (t *Tailscale) Get(ref Ref, dstPath string) error {
	return t.GetWithProgress(ref, dstPath, nil)
}

// ProgressFunc receives (bytesDone, totalBytes); total is -1 if unknown.
type ProgressFunc func(done, total int64)

// GetWithProgress fetches a bundle and reports download progress.
func (t *Tailscale) GetWithProgress(ref Ref, dstPath string, cb ProgressFunc) error {
	name := ref.Location
	if i := lastColon(name); i >= 0 {
		name = name[i+1:]
	}
	u := t.base() + "/bundles/" + url.PathEscape(name)
	resp, err := t.HTTP.Get(u)
	if err != nil {
		return fmt.Errorf("fetch from %s: %w", t.Peer, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch failed: %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	src := io.Reader(resp.Body)
	if cb != nil {
		src = &progressReader{r: resp.Body, total: resp.ContentLength, cb: cb}
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// progressReader reports byte progress about every megabyte, plus on EOF.
type progressReader struct {
	r          io.Reader
	done, last, total int64
	cb         ProgressFunc
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.done-p.last >= 1<<20 || err != nil {
		p.last = p.done
		p.cb(p.done, p.total)
	}
	return n, err
}

func (t *Tailscale) List() ([]Ref, error) {
	resp, err := t.HTTP.Get(t.base() + "/bundles")
	if err != nil {
		return nil, fmt.Errorf("list from %s: %w", t.Peer, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list failed: %s", resp.Status)
	}
	var listing []struct {
		Name string          `json:"name"`
		Meta bundle.Metadata `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(listing))
	for _, l := range listing {
		refs = append(refs, Ref{ID: l.Meta.ID, Location: t.Peer + ":" + l.Name, Meta: l.Meta})
	}
	return refs, nil
}

func lastColon(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}
