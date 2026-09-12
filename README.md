# ClipShare Server

A LAN clipboard-sharing daemon written in Go. It runs on a desktop (Linux or
Windows), watches the clipboard, and pushes changes to connected clients over
WebSocket — so a copy on your desktop appears on your phone (and vice versa).

The Android companion app lives in the separate repo
[`clipshare_android`](../clipshare_android) and auto-discovers the daemon, so no
IP configuration is needed.

That Android app can also run in **server mode**, letting two Android devices
share a clipboard directly without a desktop daemon. In server mode the phone
advertises itself via mDNS/UDP and accepts inbound WebSocket connections from
other ClipShare clients. TLS is supported using a CA + server certificate
generated on the phone itself; the CA certificate is exported and imported on
the peer device.

## Features

- **WebSocket server** on port `40403` with a **localhost control API** on `40405`
- Watches the desktop clipboard and broadcasts changes to every connected client
- **Loop protection** — never rebroadcasts content it wrote itself
- **Zero-config discovery** — mDNS (`_clipshare._tcp`) + UDP beacons on `40404`
  so the phone connects automatically
- **Wi-Fi Direct advertisement** — publishes the WebSocket endpoint on an
  already-formed P2P interface, so Android devices can share without a router
- **One-shot `send`** — push text (or your current clipboard) without keeping a
  daemon running; it starts transiently, delivers, and exits
- Cross-platform: **Linux** (Wayland + X11) and **Windows**
- Optional shared **token** authentication
- Desktop-to-desktop peer relay via the `peers` config

## Requirements

- **Go 1.26+** to build from source
- **Linux**: `wl-copy`/`wl-paste` (Wayland) **or** `xclip`/`xsel` (X11)
- **Windows**: nothing extra

## Build

```sh
# Linux
make build                # -> ./clipshare

# Windows (cross-compile from Linux)
make build-windows        # -> ./clipshare.exe

# or with Go directly
go build -o clipshare ./src/cmd/clipshare           # current platform
GOOS=windows GOARCH=amd64 go build -o clipshare.exe ./src/cmd/clipshare
```

## Install to your PATH

### Linux

```sh
make install              # symlinks ~/.local/bin/clipshare -> repo binary
```

Make sure `~/.local/bin` is on your PATH (add to `~/.zshrc` / `~/.bashrc`):

```sh
export PATH=$HOME/.local/bin:$PATH
source ~/.zshrc
```

Alternatively use `go install` (puts the binary in `GOBIN`, or in the `bin`
directory of `GOPATH`, usually `~/go/bin`):

```sh
go install ./src/cmd/clipshare
```

### Windows

For normal users, download the Windows installer from the project release page.
It installs ClipShare for the current user, asks for administrator permission
to configure the Private-network firewall rules, creates the config in
`%APPDATA%\clipshare`, and starts a Task Scheduler logon task in the same
interactive desktop session as the clipboard. This is the recommended setup;
ClipShare should not run as a Windows service when clipboard capture is needed.

See [Windows installation](docs/windows.md) for manual installation, firewall
rules, task management, upgrades, and troubleshooting.

For developers, install the current Windows build with Go. `go install` writes
to `GOBIN`, or otherwise to the `bin` directory of `GOPATH`.

```powershell
# from the repository root
# build a native Windows binary
GOOS=windows GOARCH=amd64 go build -o clipshare.exe .\src\cmd\clipshare

# install it from source into GOBIN or GOPATH\bin
go install .\src\cmd\clipshare

# show the directories Go uses for installed binaries
go env GOBIN
go env GOPATH

# verify the command is on PATH
clipshare --help
```

If `clipshare` is not found, add the Go binary directory to `PATH`. When
`GOBIN` is empty, that directory is `Join-Path (go env GOPATH) "bin"`.

> On Windows, the clipboard watcher must run in the same user desktop
> session that owns the interactive clipboard. That means the recommended
> background shape is a login task, not a Windows service session.

## Usage

```
clipshare daemon [--no-watch]   run server + clipboard watcher (foreground)
clipshare send [<text>]         push text (or your clipboard) to peers.
                                Sends via a running daemon, or starts a
                                transient one-shot server if none is running.
                                Use --oneshot to force the transient server.
clipshare watch [--timeout d]   temporarily listen (max 120s) and write the
                                first incoming push to the local clipboard,
                                then exit
clipshare status                show daemon status + connected clients
 clipshare config --init         write default config to the platform config path
clipshare cert                  manage mTLS certificates (init/issue/export/qr/list)
```

### Sending text

**One-shot (no daemon left running)** — the recommended way:

```sh
clipshare send "hello from the laptop"   # explicit text
clipshare send                           # sends your current clipboard
clipshare send --oneshot "hello"         # force the transient server
```

`send` checks for a running daemon; if none exists it starts a transient
one-shot server, waits up to 120s for a client (the phone's sync service
auto-connects via discovery), delivers the text, and exits ~5s later. Nothing
stays running.

**Persistent daemon** — for always-on sync (clipboard watching + receiving):

```sh
clipshare daemon &          # or a systemd user unit, see below
clipshare send "hello"      # push from a second terminal / keybinding
clipshare status            # verify the daemon is up and who is connected
```

`send` pushes via the running daemon when present and falls back to the
transient one-shot server otherwise; `--oneshot` forces the transient server.

### Receiving on the laptop

- **Always-on:** run `clipshare daemon` — it watches for remote content and
  writes it straight to the local clipboard (no extra command needed).
- **On demand:** run `clipshare watch` to temporarily listen (max 120s,
  `--timeout` to change). The first push from your phone is written to
  the local clipboard and printed, then it exits.
- Copying on the phone pushes while the ClipShare app is open on the phone
  (Android 10+ blocks background clipboard reads; enable background capture in
  the app's settings to sync from other apps).

## Config

The platform config path, generated with `clipshare config --init`:

- Linux: `~/.config/clipshare/config.toml`
- Windows: `%APPDATA%\clipshare\config.toml`

| key          | default    | meaning                                        |
|--------------|------------|------------------------------------------------|
| `device_name`| hostname   | name shown to clients                          |
| `mdns`       | `true`     | advertise via mDNS `_clipshare._tcp`           |
| `broadcast`  | `5`        | UDP beacon interval in seconds                 |
| `watch`      | `300`      | fallback clipboard poll interval in ms (used only when change events are unavailable, e.g. headless) |
| `token`      | `""`       | shared secret; if set, clients must send it    |
| `peers`      | `[]`       | desktop-to-desktop peer hosts (`ip[:port]`)    |
| `max_image_bytes` | `10485760` | drop images larger than this when broadcasting |
| `connection.mode` | `"discover"` | `discover` or `whitelist` (see docs)      |
| `connection.whitelist` | `[]`  | allowed devices by name/ip in whitelist mode |
| `[server] port` | `40403` | WebSocket port                              |
| `[api] port`    | `40405` | localhost control API                       |
| `[tls] enabled/ca/cert/key/verify_hostname` | `false` | mutual TLS (see docs) |
| `[log] level`   | `"info"` | log verbosity: `debug` / `info` / `warn` / `error` |
| `[log] file`    | `""`     | log file path (empty = stderr)              |

Ports: WS/TCP `40403`, UDP beacon `40404`, localhost API `40405` (127.0.0.1 only).

When a Wi-Fi Direct group has already been formed by the operating system, the
daemon advertises itself on the P2P interface automatically. See
[Wi-Fi Direct configuration](docs/configuration.md#wifi-direct).

> **Full configuration reference:** [docs/configuration.md](docs/configuration.md) —
> every key, both connection modes, and how to set up mutual TLS with
> `clipshare cert`.

## Run as a background service (Linux, systemd user unit)

`~/.config/systemd/user/clipshare.service`:

```ini
[Unit]
Description=ClipShare clipboard daemon
After=graphical-session.target

[Service]
Type=simple
ExecStart=/home/YOU/.local/bin/clipshare daemon
Restart=on-failure

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now clipshare
systemctl --user status clipshare
```

## Run ClipShare automatically on Windows

The Windows clipboard notification path depends on the interactive user
session. Use the [Windows installation guide](docs/windows.md), which covers
the installer and the equivalent manual `schtasks` setup. A scheduled logon
task is required for clipboard capture; a Windows service session is not a
reliable fit. If you only need a network daemon without local clipboard
watching, `daemon --no-watch` can be used instead.

## Security

- **Mutual TLS (mTLS):** optional but recommended. `clipshare cert init`
  creates a private CA; `clipshare cert issue` signs a server cert and client
  certs; `clipshare cert export` produces a `.p12` to import on the phone, or
  `clipshare cert qr` prints a QR code the ClipShare app can scan to import it
  (the QR bundles the key, certificate and CA, so a single scan enables mTLS
  and trusts the server). When
  `tls.enabled = true` the server requires a CA-signed client certificate and
  clients verify the server against the same CA. Hostname/IP matching is
  controlled by `tls.verify_hostname` (default `true`, strict); set it to
  `false` so certificates stay valid across wifi/DHCP changes
  (see [docs/configuration.md](docs/configuration.md)).
- **Token:** a shared secret passed as `?token=` (encrypted under `wss`).
  Without TLS it travels in plaintext on the URL.
- **Whitelist mode:** set `connection.mode = "whitelist"` to stop advertising
  and only accept (desktop) / connect to (phone) whitelisted devices.
- The control API listens on `127.0.0.1` only.

## Android server mode

The [`clipshare_android`](../clipshare_android) app can switch from **Client**
to **Server** mode in Settings. When server mode is active:

- The phone listens for WebSocket connections on port `40403`.
- It advertises itself via mDNS `_clipshare._tcp` and UDP beacons on `40404`.
- Other Android (or desktop) clients can discover and connect to it.
- Clipboard changes are relayed between all connected clients.

### TLS between Android devices

1. On the server phone, enable **TLS** in Server settings. The app generates a
   local CA + server certificate (or tap **Regenerate cert** to reissue).
2. On the server phone, tap **Show client cert QR** — it encodes a fresh
   client certificate and the CA in a single scan — or **Share .p12 bundle** to
   export the certificate bundle as a file.
3. On the client phone, scan the QR (or import the `.p12`) under **Client
   settings**. One import installs the client certificate and trusts the
   server's CA.
4. The client can now connect to the Android server over `wss://`.

Mutual TLS is always required when server TLS is enabled: the client must
present a certificate signed by the server's CA, and the server verifies the
client against that same CA.

## Protocol

JSON over WebSocket (port `40403`):

| message     | payload                             |
|-------------|-------------------------------------|
| `hello`     | `{name, platform, version}`         |
| `clipboard` | `{type, text?, data?, mime?, ts, from}` — `type` is `"text"` (default) or `"image"` (base64 `data`) |
| `ping`/`pong` | —                                 |
| `error`     | `{code, msg}`                       |

## Project layout

```
src/cmd/clipshare/        entry point (thin main)
src/internal/cli/         command dispatch + App lifecycle (daemon, send, ...)
src/internal/clip/        clipboard backends (Linux/Wayland, Windows) + watcher
src/internal/config/      TOML config loading/saving
src/internal/certs/       private CA + certificate issuance (clipshare cert)
src/internal/consts/      non-configurable application constants
src/internal/discover/    mDNS advertising + UDP beacon broadcasting
src/internal/log/         leveled logging (level + file from config)
src/internal/protocol/    JSON-over-WebSocket wire types + codec
src/internal/websocket/   WebSocket server + outbound peers
src/internal/api/         localhost HTTP control API + client
src/internal/version/     release version
```
