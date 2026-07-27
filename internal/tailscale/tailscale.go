// Package tailscale shells out to the Tailscale CLI for tailnet identity and
// peer discovery. We use the CLI rather than tsnet so synckit rides the user's
// existing, already-authenticated tailscaled instance instead of joining the
// tailnet as its own node.
package tailscale

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Peer is one online tailnet machine.
type Peer struct {
	Host   string // MagicDNS short name, e.g. "laptop"
	DNS    string // full MagicDNS name
	IP     string // first tailnet IPv4 (100.x)
	OS     string
	Online bool
	Self   bool
}

// statusJSON is the subset of `tailscale status --json` we consume.
type statusJSON struct {
	Self  *node            `json:"Self"`
	Peer  map[string]*node `json:"Peer"`
	MagicDNSSuffix string  `json:"MagicDNSSuffix"`
}

type node struct {
	HostName    string   `json:"HostName"`
	DNSName     string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	OS          string   `json:"OS"`
	Online      bool     `json:"Online"`
}

// Available reports whether the tailscale CLI is present and reachable.
func Available() bool {
	_, err := run("version")
	return err == nil
}

// SelfIP returns this machine's first tailnet IPv4 address.
func SelfIP() (string, error) {
	out, err := run("ip", "-4")
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	if ip == "" {
		return "", fmt.Errorf("no tailnet IPv4 (is tailscale up?)")
	}
	return ip, nil
}

// Peers lists tailnet machines (self excluded unless includeSelf).
func Peers(includeSelf bool) ([]Peer, error) {
	out, err := run("status", "--json")
	if err != nil {
		return nil, err
	}
	var st statusJSON
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		return nil, err
	}
	var peers []Peer
	if includeSelf && st.Self != nil {
		peers = append(peers, toPeer(st.Self, true))
	}
	for _, n := range st.Peer {
		peers = append(peers, toPeer(n, false))
	}
	return peers, nil
}

// Resolve turns a user-supplied peer name into a dialable host (IP preferred),
// matching against short hostname, MagicDNS name, or IP.
func Resolve(name string) (string, error) {
	peers, err := Peers(false)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	for _, p := range peers {
		if p.IP == name || p.Host == name || p.DNS == name || strings.HasPrefix(p.DNS, name+".") {
			if p.IP != "" {
				return p.IP, nil
			}
			return p.DNS, nil
		}
	}
	// Fall back to using it verbatim (may be a raw IP or DNS name).
	return name, nil
}

func toPeer(n *node, self bool) Peer {
	ip := ""
	if len(n.TailscaleIPs) > 0 {
		ip = n.TailscaleIPs[0]
	}
	return Peer{
		Host:   strings.TrimSuffix(n.HostName, "."),
		DNS:    strings.TrimSuffix(n.DNSName, "."),
		IP:     ip,
		OS:     n.OS,
		Online: n.Online,
		Self:   self,
	}
}

var (
	binOnce sync.Once
	binPath string
)

// resolveBin finds the tailscale CLI. It checks PATH first, then the standard
// per-OS install locations — critical on macOS, where the CLI ships INSIDE the
// app bundle and is not on PATH by default (so a plain "tailscale" exec fails
// and no peers are ever found), and on Windows where it lives under Program
// Files. GUI apps launched from Finder/Explorer also get a minimal PATH, so we
// must probe absolute paths regardless.
func resolveBin() string {
	binOnce.Do(func() {
		if p, err := exec.LookPath("tailscale"); err == nil {
			binPath = p
			return
		}
		var candidates []string
		switch runtime.GOOS {
		case "darwin":
			candidates = []string{
				"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
				"/usr/local/bin/tailscale",
				"/opt/homebrew/bin/tailscale",
			}
		case "windows":
			pf := os.Getenv("ProgramFiles")
			if pf == "" {
				pf = `C:\Program Files`
			}
			candidates = []string{filepath.Join(pf, "Tailscale", "tailscale.exe")}
		default: // linux and others
			candidates = []string{
				"/usr/bin/tailscale",
				"/usr/local/bin/tailscale",
				"/var/lib/tailscale/tailscale", // some packaged installs
			}
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				binPath = c
				return
			}
		}
	})
	return binPath
}

func run(args ...string) (string, error) {
	bin := resolveBin()
	if bin == "" {
		return "", fmt.Errorf("tailscale CLI not found on PATH or standard install locations " +
			"(on macOS, symlink it: sudo ln -s /Applications/Tailscale.app/Contents/MacOS/Tailscale /usr/local/bin/tailscale)")
	}
	out, err := exec.Command(bin, args...).Output()
	if err != nil {
		return "", fmt.Errorf("tailscale %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
