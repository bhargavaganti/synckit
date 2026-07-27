// Package tailscale shells out to the Tailscale CLI for tailnet identity and
// peer discovery. We use the CLI rather than tsnet so synckit rides the user's
// existing, already-authenticated tailscaled instance instead of joining the
// tailnet as its own node.
package tailscale

import (
	"encoding/json"
	"fmt"
	"net"
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

// SelfIP returns this machine's first tailnet IPv4 address. It validates that
// the CLI actually returned an IP: the macOS App Store Tailscale sometimes
// prints an error to stdout with exit 0 ("The Tailscale GUI failed to start"),
// which must NOT be mistaken for an address to bind.
func SelfIP() (string, error) {
	out, err := run("ip", "-4")
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("tailscale did not return a valid IP (got %q) — is tailscale up, and is this a working CLI?", firstLine(out))
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
	binOnce     sync.Once
	autoBin     string
	explicitBin string // user override (Settings / SetBinPath), highest priority
)

// SetBinPath sets an explicit path to the tailscale CLI, overriding all
// auto-detection. Empty clears the override. Used by the Settings UI.
func SetBinPath(p string) { explicitBin = strings.TrimSpace(p) }

// candidates lists the standard per-OS install locations we probe.
func candidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
			"/usr/local/bin/tailscale",
			"/opt/homebrew/bin/tailscale",
		}
	case "windows":
		pf := os.Getenv("ProgramFiles")
		if pf == "" {
			pf = `C:\Program Files`
		}
		return []string{filepath.Join(pf, "Tailscale", "tailscale.exe")}
	default:
		return []string{"/usr/bin/tailscale", "/usr/local/bin/tailscale", "/var/lib/tailscale/tailscale"}
	}
}

// resolveBin finds the tailscale CLI in priority order: explicit override, the
// SYNCKIT_TAILSCALE env var, PATH, then the standard per-OS locations. The
// per-OS probe is critical on macOS (CLI ships inside the app bundle, off PATH)
// and for GUI apps launched from Finder/Explorer (minimal PATH).
func resolveBin() string {
	if explicitBin != "" {
		return explicitBin
	}
	if e := strings.TrimSpace(os.Getenv("SYNCKIT_TAILSCALE")); e != "" {
		return e
	}
	binOnce.Do(func() {
		if p, err := exec.LookPath("tailscale"); err == nil {
			autoBin = p
			return
		}
		for _, c := range candidates() {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				autoBin = c
				return
			}
		}
	})
	return autoBin
}

// BinPath returns the resolved tailscale binary path (or "" if none found).
func BinPath() string { return resolveBin() }

// Diagnose returns human-readable diagnostics for the Settings/debug console.
func Diagnose() string {
	var b strings.Builder
	bin := resolveBin()
	if bin == "" {
		fmt.Fprintln(&b, "tailscale CLI: NOT FOUND")
		fmt.Fprintln(&b, "checked: PATH")
		for _, c := range candidates() {
			fmt.Fprintf(&b, "  %s\n", c)
		}
		fmt.Fprintln(&b, "→ set the path in Settings, or export SYNCKIT_TAILSCALE=/path/to/tailscale")
		return b.String()
	}
	fmt.Fprintf(&b, "tailscale CLI: %s\n", bin)
	hadErr := false
	verOut, verErr := run("version")
	if verErr == nil {
		fmt.Fprintf(&b, "version: %s\n", firstLine(verOut))
	} else {
		hadErr = true
		fmt.Fprintf(&b, "version: ERROR %v\n", verErr)
	}
	if ip, err := SelfIP(); err == nil {
		fmt.Fprintf(&b, "tailnet IP: %s\n", ip)
	} else {
		hadErr = true
		fmt.Fprintf(&b, "tailnet IP: ERROR %v\n", err)
	}
	if peers, err := Peers(false); err == nil {
		fmt.Fprintf(&b, "peers found: %d\n", len(peers))
		for _, p := range peers {
			state := "offline"
			if p.Online {
				state = "online"
			}
			fmt.Fprintf(&b, "  %-20s %-16s %-8s %s\n", p.Host, p.IP, p.OS, state)
		}
	} else {
		hadErr = true
		fmt.Fprintf(&b, "peers: ERROR %v\n", err)
	}

	// The macOS App Store Tailscale ships a CLI that only works when its app is
	// running & connected, and often fails with "CLIError". Point users at a
	// reliable CLI.
	if hadErr && (strings.Contains(bin, "Tailscale.app") ||
		strings.Contains(verOut, "CLIError") || strings.Contains(verOut, "GUI failed to start")) {
		fmt.Fprintln(&b, "\nhint: this looks like the macOS App Store Tailscale, whose CLI is unreliable.")
		fmt.Fprintln(&b, "  1) make sure the Tailscale app is running & shows Connected, then retry; or")
		fmt.Fprintln(&b, "  2) install a working CLI:  brew install tailscale")
		fmt.Fprintln(&b, "     then set its path here, e.g. /opt/homebrew/bin/tailscale")
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return strings.TrimSpace(s)
}

func run(args ...string) (string, error) {
	bin := resolveBin()
	if bin == "" {
		return "", fmt.Errorf("tailscale CLI not found on PATH or standard install locations " +
			"(set the path in Settings, or export SYNCKIT_TAILSCALE=/path/to/tailscale)")
	}
	out, err := exec.Command(bin, args...).Output()
	if err != nil {
		return "", fmt.Errorf("tailscale %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
