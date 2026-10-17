# Windows server ↔ Android Client over Wi-Fi Direct with mTLS

Secure direct link. The phone keeps mobile data for internet while ClipShare
uses the P2P group. Only a client with a certificate from this server's CA can
connect.

## Server prerequisites

Windows daemon 2.2.0 or later. It advertises as a Wi-Fi Direct autonomous
group owner. Required config in `%APPDATA%\clipshare\config.toml`:

```toml
[server]
bind = "0.0.0.0"
port = 40403

[wifi_direct]
enabled = true

[tls]
enabled = true
verify_hostname = false
```

`verify_hostname = false` allows the dynamic group-owner IP
(`192.168.137.1` by default). Chain validation against `[tls].ca` still runs.

Firewall (installed automatically on fresh installs and upgrades):

- `ClipShare WiFi Direct TCP 40403` — `profile=any remoteip=localsubnet`
- `ClipShare WiFi Direct P2P TCP 40403` — `profile=any remoteip=192.168.137.0/24`

## Issue the Pixel identity once per phone

Run on Windows. Do not re-run `cert init` if a CA already exists.

```powershell
clipshare cert list
clipshare cert issue --name pixel-9 --type client
clipshare cert export --name pixel-9 --type client --out "$env:APPDATA\clipshare\certs\pixel-9-client.p12"
clipshare cert qr --name pixel-9 --type client --out "$env:APPDATA\clipshare\certs\pixel-9-client.png"
```

What you share:

- `pixel-9-client.png` — scan with Android **Scan client secret**
- `pixel-9-client.p12` — import with Android **Select client secret**, password `clipshare`

Keep `ca.key` and `server.key` on Windows. The bundle contains the client
private key, client certificate, and CA certificate.

## Android Client steps

1. Client mode, server host blank, server port `40403`.
2. Enable **Wi-Fi Direct** and **TLS (wss)**.
3. Import the client credential first, then save.
4. Wi-Fi radio on, no access-point join required.
5. Start sync. The app refuses P2P without TLS + imported client cert
   (Android 1.1.6+).

## Verify

Windows log `%APPDATA%\clipshare\logs\clipshare.log` should show:

```text
wss server listening on 0.0.0.0:40403
mdns: advertising <HOST>._clipshare._tcp on port 40403 (tls=true)
wifi direct: Windows peer advertiser status: Started
wifi direct: advertising <HOST> on 192.168.137.1
wifi direct: Windows peer connected (status=Connected)
```

Android shows connected over `wifi_direct`, then use **Text to push →
Send to server**.

> Status: P2P group formation is verified, but the `wss://192.168.137.1:40403`
> data path is still timing out in live testing despite both firewall rules
> being present. Verify the P2P rule with the `netsh ... show rule`
> command above before testing clipboard sync.
