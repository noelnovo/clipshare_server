# Linux setup

## LAN server

Start the daemon with the default listener (`0.0.0.0:40403`) and discovery
enabled. Android Client mode can discover it using mDNS/UDP. For TLS, configure
the server's CA and identity under `[tls]`, set `enabled = true`, and import a
client certificate issued by that CA into the Android app.

## Wi-Fi Direct

The Go daemon does **not** create or join a Linux Wi-Fi Direct group. Linux must
form/join the P2P group through the system Wi-Fi stack or a separate platform
tool first. The daemon then detects an active P2P interface and publishes its
WebSocket endpoint on that interface. If auto-detection misses it, set the
interface name explicitly:

```toml
[server]
bind = "0.0.0.0"

[wifi_direct]
enabled = true
interfaces = ["p2p-wlan0-0"]
```

Check the current interfaces and addresses with `ip -br addr`. Automatic
detection recognizes names containing `p2p` or Wi-Fi Direct and common P2P
addresses such as `192.168.49.x` and `192.168.137.x`.

An interface and mDNS record alone do not establish the group. The Android
device must also be able to discover and join the Linux P2P peer. Linux
group-owner advertising/acceptance varies by Wi-Fi driver and desktop stack, so
use the system-specific WFD setup before starting the Android client.

## mTLS over Wi-Fi Direct

Generate a CA once, then issue the Linux server identity and a separate client
identity for the Android phone:

```sh
clipshare cert init                                  # omit if this config already has a CA
clipshare cert issue --name "$(hostname)" --type server
clipshare cert issue --name pixel-9 --type client
clipshare cert qr --name pixel-9 --type client --out pixel-9-client.png
```

Set `tls.enabled = true` and `tls.verify_hostname = false` in the Linux config
when the group-owner IP is dynamic. The Android app imports `pixel-9-client.png`
using **Scan client secret**, enables Client TLS, and enables Wi-Fi Direct. The
server requires the imported client certificate; the QR also installs the CA
needed to verify the Linux server. Keep `ca.key` on the server and distribute
only the client QR or `.p12` bundle.

Without TLS, the link uses WebSocket plaintext. Wi-Fi Direct's WPA2 link
encryption protects the radio link but does not give ClipShare a TLS client
identity.
