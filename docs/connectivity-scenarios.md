# ClipShare connection scenarios

This guide distinguishes the **ClipShare role** (which endpoint listens for the
WebSocket) from the **Wi-Fi Direct role** (which device owns or joins the radio
group). A WebSocket is full-duplex after it connects: a ClipShare client can
send clipboard data to the server, and the server can send clipboard data back.

## Scenario matrix

| Devices | ClipShare roles | Wi-Fi Direct setup | Status |
|---|---|---|---|
| Windows daemon ↔ Android phone | Windows server, Android Client | Windows advertises a group-owner peer; Android discovers and joins | Group formation verified (Android gets a P2P IP, Windows logs peer connected). IP/TCP data path is under diagnosis: `wss://192.168.137.1:40403` currently times out despite the daemon listening and both firewall rules present. |
| Android phone ↔ Android phone | One Android Server, one Android Client | Android Server creates the group and advertises the ClipShare P2P service; Client discovers and joins | Supported by the Android P2P service path. |
| Linux daemon ↔ Android phone | Linux server, Android Client | Linux must create/join a P2P group outside the daemon; daemon advertises on its P2P interface | Server/WebSocket path is supported after the OS has formed the group. Group creation is not built into the Linux daemon. |
| Windows daemon ↔ Windows daemon (LAN) | Both daemons; configure outbound `peers` | Ordinary LAN | Supported. Configure each daemon's `peers` with the other daemon's address for bidirectional relay. |
| Windows daemon ↔ Linux daemon (LAN) | Both daemons; configure outbound `peers` | Ordinary LAN | Supported with the same static-peer configuration. |
| Desktop daemon ↔ desktop daemon (Wi-Fi Direct) | Both daemons; one must dial the other | OS-managed P2P group plus static peer address | Not automatic. Windows currently advertises as a group owner, not a P2P client; Linux group setup is OS-managed. |
| Android Client ↔ Android Client | No listener | Any | Not supported directly. Choose one phone as **Server** and the other as **Client**. |

The Windows/Android P2P path uses regular peer discovery to negotiate a group;
it does not rely on Windows Wi-Fi Direct Services discovery. Android's Bonjour
P2P service discovery and Windows' Wi-Fi Direct Services protocol differ.

## Per-pairing guides

- [Windows ↔ Android over Wi-Fi Direct with mTLS](scenarios/windows-android-wifi-direct-mtls.md)
- [Windows ↔ Android on a LAN](scenarios/windows-android-lan.md)
- [Linux ↔ Android over Wi-Fi Direct](scenarios/linux-android-wifi-direct.md)
- [Android ↔ Android](scenarios/android-android-wifi-direct.md)
- [Desktop ↔ Desktop](scenarios/desktop-desktop.md)

## No-TLS setup (LAN testing only)

No TLS means the ClipShare WebSocket payload is not encrypted or authenticated.
The Wi-Fi Direct group has its own link encryption, but that does not authenticate
ClipShare clients. Use this only for an isolated LAN test. Android 1.1.6 and
later refuses to form or use a Wi-Fi Direct ClipShare connection without TLS
and an imported client certificate.

### Windows daemon ↔ Android Client on a LAN

The Windows daemon config should include:

```toml
[server]
bind = "0.0.0.0"
port = 40403

[wifi_direct]
enabled = true

[tls]
enabled = false
```

On Android, use Client mode, leave the manual server host blank, set port
`40403`, and leave Client TLS off. This plaintext setup is LAN-only; the Android
app refuses P2P endpoints when TLS is off.

For Linux, keep the same server/TLS settings, but create the Wi-Fi Direct group
with Linux networking tools first. The daemon will publish on the resulting
P2P interface.

### Android Server ↔ Android Client on a LAN

Set one phone to Server mode and the other to Client mode. Leave Server TLS and
Client TLS off and use the default port `40403`. Use a LAN for this plaintext
test; the Android app requires mTLS for Wi-Fi Direct.

## mTLS setup

With server TLS enabled, ClipShare uses mutual TLS: the server presents its
server identity and **requires a client certificate signed by its CA**. The
client verifies the server certificate chain and presents its own client
identity. A CA certificate alone is not a client identity.

The daemon's TLS setting is per WebSocket endpoint, so enabling it secures that
server over both LAN and Wi-Fi Direct. Android 1.1.6 and later fails closed for
Wi-Fi Direct unless TLS is announced and a client certificate is imported. For
the Windows peer fallback (where there is no TLS record before joining), enable
Client TLS in the app too. Do not treat WPA2 link encryption as a replacement
for mTLS.

### Windows or Linux server ↔ Android Client

On the desktop server, initialize the CA only once, then issue a server identity
and a separate Android client identity:

```powershell
clipshare cert init                                  # omit if this CA already exists
clipshare cert issue --name $env:COMPUTERNAME --type server
clipshare cert issue --name pixel-9 --type client
clipshare cert qr --name pixel-9 --type client --out "$env:APPDATA\clipshare\certs\pixel-9-client.png"
```

Linux uses the same commands (replace `$env:COMPUTERNAME` with `$(hostname)` and
use `~/.config/clipshare/certs` for the generated files). Enable TLS in the
server config:

```toml
[tls]
enabled = true
verify_hostname = false
```

`verify_hostname = false` permits the dynamically assigned P2P IP; certificate
chain validation against the private CA remains enabled. On Android, enable
Client TLS and import the client QR with **Scan client secret** (or import the
exported `.p12`). The QR/.p12 contains the client's private key, client
certificate, and CA certificate. Never copy `ca.key` to the phone.

The Windows installer automates CA/server/client certificate creation and
produces `%APPDATA%\clipshare\certs\android-client.png` on a fresh installation.
It enables TLS for that new installation. Upgrades preserve an existing config;
they do not silently change an existing TLS setting.

### Android Server ↔ Android Client

On the Server phone, enable Server TLS. The app creates a local CA and server
certificate and requires CA-signed client certificates. On the Client phone,
enable Client TLS and scan **Show client secret** from the Server phone. The QR
issues a new client key/certificate signed by that server's CA and imports the
CA at the same time. Repeat this for each different Android server CA.

### Windows/Linux daemon ↔ Windows/Linux daemon

Both daemons must trust the same CA and present certificates valid for both
server and client authentication. Issue a server certificate for each daemon
from one CA and configure each daemon's `[tls] ca`, `[tls] cert`, and `[tls] key`
paths. Enable TLS on both; configure mutual `peers` addresses if both directions
should relay clipboard updates. Set `tls.verify_hostname = false` when peer
addresses are dynamic.

For a plaintext LAN test, set `tls.enabled = false` on both daemons. This has no
client-certificate checks and should not be used over untrusted networks.

## Troubleshooting the layers

1. **Discovery/group:** Windows logs `Windows peer advertiser status: Started`
   and `advertising ... on 192.168.137.1`; Android must form a P2P group and get
   a group-owner address.
2. **IP route:** Android's P2P interface should receive an address on the same
   group subnet. On Windows, the WebSocket listener must bind to `0.0.0.0` or
   the P2P adapter address.
3. **TCP/firewall:** allow inbound TCP `40403` from the P2P subnet. The Windows
   installer installs a Wi-Fi Direct rule for `192.168.137.0/24` (and a local
   subnet rule for other P2P ranges) on both fresh installs and upgrades.
   Verify with `netsh advfirewall firewall show rule name="ClipShare WiFi Direct P2P TCP 40403" verbose`.
   Local `Test-NetConnection 192.168.137.1 -Port 40403` should succeed; if the
   phone still times out while the group is up, capture the firewall drop log
   before changing rules.
4. **TLS:** both ends must agree on `ws://` versus `wss://`; for mTLS, the
   Android client must import a client identity issued by the server's CA.
5. **Clipboards:** after the Android Client shows connected, send a short test
   using the app's **Text to push → Send to server** controls. Background reads
   from other apps require Android's background clipboard/accessibility option.
