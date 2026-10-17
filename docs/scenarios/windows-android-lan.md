# Windows server ↔ Android Client on a LAN

Same infrastructure network. Covers both mTLS and plaintext test mode.

## With mTLS (recommended)

Server config:

```toml
[server]
bind = "0.0.0.0"
port = 40403

[tls]
enabled = true
verify_hostname = false
```

Issue/import exactly as in
`windows-android-wifi-direct-mtls.md`:

```powershell
clipshare cert issue --name pixel-9 --type client
clipshare cert export --name pixel-9 --type client --out "$env:APPDATA\clipshare\certs\pixel-9-client.p12"
clipshare cert qr --name pixel-9 --type client --out "$env:APPDATA\clipshare\certs\pixel-9-client.png"
```

Android: Client mode, discovery on, **TLS (wss)** on, imported client cert,
port `40403`. Manual host optional.

## Plaintext LAN test only

Server:

```toml
[server]
bind = "0.0.0.0"

[tls]
enabled = false
```

Android: Client mode, **TLS (wss)** off, discovery on.

Do not use plaintext over untrusted networks. Android 1.1.6+ still refuses
Wi-Fi Direct plaintext; this section is LAN-only.
