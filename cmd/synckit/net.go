package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bhargav/synckit/internal/daemon"
	"github.com/bhargav/synckit/internal/restore"
	"github.com/bhargav/synckit/internal/service"
	"github.com/bhargav/synckit/internal/syncengine"
	ts "github.com/bhargav/synckit/internal/tailscale"
	"github.com/bhargav/synckit/internal/transport"
	"github.com/bhargav/synckit/internal/ui"
)

func newPeersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "peers",
		Short: "List online tailnet machines running (or reachable by) synckit",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !ts.Available() {
				return fmt.Errorf("tailscale CLI not found; install Tailscale and run `tailscale up`")
			}
			peers, err := ts.Peers(false)
			if err != nil {
				return err
			}
			if len(peers) == 0 {
				fmt.Println("no tailnet peers found")
				return nil
			}
			for _, p := range peers {
				state := "offline"
				if p.Online {
					state = "online"
				}
				fmt.Printf("%-20s %-16s %-8s %s\n", p.Host, p.IP, p.OS, state)
			}
			return nil
		},
	}
}

func newPushCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "push <bundle.zip> <peer>",
		Short: "Send a bundle to a peer's synckit daemon over the tailnet",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundlePath, peer := args[0], args[1]
			host, err := ts.Resolve(peer)
			if err != nil {
				return err
			}
			tr := transport.NewTailscale(host, port)
			ref, err := tr.Put(bundlePath)
			if err != nil {
				return err
			}
			fmt.Printf("pushed %s → %s\n", ref.ID, peer)
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", transport.DefaultPort, "daemon port on the peer")
	return cmd
}

func newPullCmd() *cobra.Command {
	var port int
	var out string
	cmd := &cobra.Command{
		Use:   "pull <peer> [bundle-name]",
		Short: "List or fetch bundles from a peer's daemon",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := ts.Resolve(args[0])
			if err != nil {
				return err
			}
			tr := transport.NewTailscale(host, port)
			if len(args) == 1 {
				refs, err := tr.List()
				if err != nil {
					return err
				}
				if len(refs) == 0 {
					fmt.Println("peer has no bundles")
					return nil
				}
				for _, r := range refs {
					fmt.Printf("%-30s %s\n", r.ID, r.Location)
				}
				return nil
			}
			name := args[1]
			dst := out
			if dst == "" {
				dst = filepath.Join(defaultSpoolDir(), name)
			}
			ref := transport.Ref{Location: host + ":" + name}
			if err := tr.Get(ref, dst); err != nil {
				return err
			}
			fmt.Printf("pulled → %s\n", dst)
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", transport.DefaultPort, "daemon port on the peer")
	cmd.Flags().StringVarP(&out, "out", "o", "", "output path for a fetched bundle")
	return cmd
}

func newServeCmd() *cobra.Command {
	var port int
	var spool string
	var bindAll bool
	var autoRestore bool
	var noUI bool
	var uiPort int
	var auto bool
	var autoApply bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the daemon (tailnet receiver) + localhost dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if spool == "" {
				spool = defaultSpoolDir()
			}
			if uiPort == 0 {
				uiPort = port
			}
			bindIP := ""
			if !bindAll {
				if !ts.Available() {
					return fmt.Errorf("tailscale CLI not found; use --bind-all to bind all interfaces (less safe)")
				}
				ip, err := ts.SelfIP()
				if err != nil {
					return fmt.Errorf("resolve tailnet IP: %w (or use --bind-all)", err)
				}
				bindIP = ip
			}

			var onReceive func(string)
			if autoRestore {
				onReceive = func(path string) {
					res, err := restore.Run(adapters(nil), restore.Options{Src: path})
					if err != nil {
						fmt.Fprintf(os.Stderr, "auto-restore %s: %v\n", path, err)
						return
					}
					for _, o := range res.Outcomes {
						if o.Restored {
							fmt.Printf("auto-restored %s/%s\n", o.App, o.Instance)
						}
					}
				}
			}

			srv, err := daemon.New(daemon.Config{
				SpoolDir:  spool,
				BindIP:    bindIP,
				Port:      port,
				OnReceive: onReceive,
			})
			if err != nil {
				return err
			}

			// Graceful shutdown on Ctrl-C.
			go func() {
				sig := make(chan os.Signal, 1)
				signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
				<-sig
				fmt.Println("\nshutting down")
				os.Exit(0)
			}()

			// Control plane: the dashboard, bound to localhost only so the
			// snapshot/restore/fetch API is never exposed to tailnet peers.
			if !noUI {
				uiSrv := ui.New(ui.Config{SpoolDir: spool, Port: port})
				addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", uiPort))
				go func() {
					fmt.Printf("dashboard:        http://%s\n", addr)
					if err := http.ListenAndServe(addr, uiSrv.Handler()); err != nil {
						fmt.Fprintf(os.Stderr, "dashboard error: %v\n", err)
					}
				}()
			}

			// Seamless engine: auto-snapshot on app-close + periodically, and
			// auto-share to serving peers. On a headless node it logs available
			// updates; --auto-apply additionally restores them (opt-in, since
			// that overwrites local profiles).
			if auto {
				svc := service.New(spool, port)
				eng := syncengine.New(svc, syncengine.Config{
					AutoSnapshot: true, AutoShare: true,
				})
				eng.OnActivity = func(s string) { fmt.Println("[sync]", s) }
				eng.OnUpdates = func(ups []syncengine.Update) {
					for _, u := range ups {
						if autoApply {
							if _, err := svc.Fetch(u.PeerIP, u.BundleName, true, false); err != nil {
								fmt.Printf("[sync] auto-apply %s from %s failed: %v\n", u.App, u.PeerHost, err)
							} else {
								fmt.Printf("[sync] auto-applied %s from %s\n", u.App, u.PeerHost)
							}
						} else {
							fmt.Printf("[sync] update available: %s has a newer %s (%s) — run restore or use the app\n",
								u.PeerHost, u.App, u.Age)
						}
					}
				}
				go eng.Run(cmd.Context())
			}

			return srv.Serve()
		},
	}
	cmd.Flags().IntVar(&port, "port", transport.DefaultPort, "listen port")
	cmd.Flags().StringVar(&spool, "spool", "", "directory to store received bundles")
	cmd.Flags().BoolVar(&bindAll, "bind-all", false, "bind all interfaces instead of the tailnet IP (less safe)")
	cmd.Flags().BoolVar(&autoRestore, "auto-restore", false, "restore bundles automatically on receipt (dangerous; opt-in)")
	cmd.Flags().BoolVar(&noUI, "no-ui", false, "disable the localhost dashboard")
	cmd.Flags().IntVar(&uiPort, "ui-port", 0, "dashboard port on localhost (default: same as --port)")
	cmd.Flags().BoolVar(&auto, "auto", false, "run the seamless engine: auto-snapshot on close + periodically, auto-share to peers")
	cmd.Flags().BoolVar(&autoApply, "auto-apply", false, "with --auto: also restore newer peer profiles automatically (overwrites local; opt-in)")
	return cmd
}
