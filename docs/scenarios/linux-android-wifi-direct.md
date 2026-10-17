# Linux server ↔ Android Client over Wi-Fi Direct

The Linux daemon does not create the P2P group. Create it with Linux Wi-Fi
tools first, then run the daemon on the resulting interface.

## With mTLS

```toml
[server]
bind = "0.0.0.0"
port = 40403

[wifi_direct]
enabled = true
# interfaces = ["p2p-wlan0-0"]  # only if auto-detection misses it

[tls]
enabled = true
verify_hostname = false
```

```sh
clipshare cert init                                # once per CA
clipshare cert issue --name "$(hostname)" --type server
clipshare cert issue --name pixel-9 --type client
clipshare cert qr --name pixel-9 --type client --out pixel-9-client.png
```

Android imports the QR/`.p12`, enables **Wi-Fi Direct** + **TLS (wss)**,
leaves host blank.

## Plaintext

Same as above with `[tls].enabled = false`, but LAN-only for testing.
Android 1.1.6+ refuses Wi-Fi Direct plaintext. Verify the P2P interface with
`ip -br addr` and check for `192.168.49.x` or `192.168.137.x`.
