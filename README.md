# synckit

Sync application profiles across macOS, Windows and Linux. Captures **Chrome**,
**Firefox** and **DBeaver** profiles into portable `.zip` bundles you can export,
import, or push machine-to-machine over a [Tailscale](https://tailscale.com)
network.

## Design

**Full profile clone.** Bundles copy each profile tree byte-for-byte (excluding
regenerable caches and lock files), so settings, bookmarks, extensions and
connection definitions all travel together.

**Secrets portability differs per app** — surfaced in `detect` and warned on
restore:

| App | Secrets cross machines? | Why |
|-----|:-----------------------:|-----|
| DBeaver | ✅ yes | credentials use a fixed AES key baked into DBeaver |
| Firefox | ✅ yes | `logins.json` decrypts with `key4.db`, which travels along |
| Chrome  | ❌ same machine only | passwords/cookies are sealed by the OS keyring (DPAPI / Keychain / kwallet), which is bound to the machine + OS user |

Restoring a Chrome profile onto a **different** machine still restores settings,
bookmarks and extensions — only the encrypted passwords/cookies are dropped by
Chrome (use Chrome's own sign-in sync for those).

**Safety rails**
- **App must be closed.** Snapshot and restore both refuse a profile whose app
  is running (lock-file check), because copying live SQLite/WAL files corrupts
  the profile. Override with `--force` at your own risk.
- **Backup before overwrite.** Restore moves the existing profile to
  `<profile>.<timestamp>.bak` before extracting, and rolls back on failure.
- **Checksum verified.** Every file's SHA-256 is recorded at snapshot and
  re-verified on restore.

## Architecture

```
cmd/synckit/            CLI (cobra): detect, snapshot, restore, peers, push, pull, serve
internal/
  app/                  Adapter interface + chrome / firefox / dbeaver
  bundle/               .zip format, metadata.json, checksums, extract+verify
  snapshot/             detect → walk (honoring excludes) → write bundle
  restore/              verify → preflight → backup → extract → warnings
  transport/            Transport interface, file (export/import), tailscale (HTTP)
  tailscale/            CLI wrapper: self IP + peer discovery
  daemon/               `serve`: receives bundles from tailnet peers
```

The **Adapter interface** is the only app-specific surface; everything above it
is generic. Adding an app = one new adapter.

The **bundle** is a single `.zip` used by *both* transports — the file exporter
and the Tailscale daemon move the exact same bytes.

## Usage

```bash
# What's on this machine?
synckit detect

# Capture everything into a bundle (skips running apps)
synckit snapshot

# Just some apps, to a chosen path
synckit snapshot --apps firefox,dbeaver -o ~/backup.zip

# Preview a restore without changing anything
synckit restore --dry-run ~/backup.zip

# Restore for real (backs up existing profiles first)
synckit restore ~/backup.zip
```

### Sync over Tailscale

On the receiving machine, run the daemon (binds to the tailnet IP; only tailnet
peers can reach it):

```bash
synckit serve
```

From the sending machine:

```bash
synckit peers                       # list online tailnet machines
synckit push ~/backup.zip laptop    # send a bundle to peer "laptop"
synckit pull laptop                 # list bundles on a peer
synckit pull laptop backup.zip      # fetch one
```

`serve --auto-restore` will apply bundles the moment they arrive (opt-in;
dangerous — it overwrites profiles automatically).

## Seamless mode

The native app (and `synckit serve --auto` on headless machines) runs a sync
engine that makes the whole thing hands-off, without ever overwriting silently:

- **Auto-snapshot** each app right after it closes, plus a periodic fallback.
- **Auto-share** the newest snapshot to every peer serving synckit.
- **Notify + one-click apply** — when a peer holds a profile newer than yours,
  the app surfaces it; *you* click Apply (your profile is backed up first).
  `serve --auto --auto-apply` opts a headless node into applying automatically.

## Native app

`cmd/synckit-gui` is a [Fyne](https://fyne.io) desktop app that bundles the GUI,
the tailnet receiver daemon, and the sync engine in one process.

A Fyne GUI links against each OS's native graphics libraries, so **it must be
built on (or for) its target OS** — it cannot be cross-compiled from another
platform the way the pure-Go CLI can. Releases are therefore produced by CI, one
native runner per OS (see `.github/workflows/release.yml`).

## Build

Pure-Go CLI (cross-compiles anywhere):

```bash
go build -o synckit ./cmd/synckit
go test ./...
```

Native GUI (needs a C compiler + platform GUI libs on the build OS):

```bash
# Linux also needs: sudo apt-get install libgl1-mesa-dev xorg-dev libxkbcommon-dev
go build ./cmd/synckit-gui                       # dev build
go install fyne.io/fyne/v2/cmd/fyne@latest       # packaging tool
cd cmd/synckit-gui && fyne package --release      # -> synckit.app / .exe / .tar.xz
```

Cross-compile the CLI for every OS:

```bash
for t in linux/amd64 darwin/arm64 darwin/amd64 windows/amd64; do
  GOOS=${t%/*} GOARCH=${t#*/} go build -o dist/synckit-${t%/*}-${t#*/} ./cmd/synckit
done
```

## Status

Working: all three adapters; snapshot/restore with backup + checksum verify;
file and Tailscale transports; receiver daemon; localhost web dashboard; native
Fyne app; seamless sync engine; CI (build/test on all three OSes) and tagged
releases. Verified on Windows against live Chrome/Firefox profiles and a full
tailnet push→receive round-trip.

Not yet done: bundle encryption at rest (snapshots are plain zip today),
login-autostart + tray, selective per-file include lists, compression tuning.
