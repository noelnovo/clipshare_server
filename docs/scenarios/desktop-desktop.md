# Desktop daemon ↔ desktop daemon

Windows ↔ Windows, Windows ↔ Linux, and Linux ↔ Linux over LAN using static
`peers`. Wi-Fi Direct desktop-to-desktop negotiation is not automatic.

## LAN with mTLS

Both daemons share one CA. Each needs a server/client-capable identity:

```sh
clipshare cert issue --name desktop-a --type server
clipshare cert issue --name desktop-b --type server
```

Each config:

```toml
[server]
bind = "0.0.0.0"
port = 40403

peers = ["<other-desktop-ip>:40403"]

[tls]
enabled = true
verify_hostname = false
```

Set mutual `peers` entries for bidirectional relay.

## LAN plaintext test only

```toml
peers = ["<other-desktop-ip>:40403"]

[tls]
enabled = false
```

## Wi-Fi Direct note

Windows advertises as group owner; it does not join another desktop as P2P
client automatically. Linux P2P setup is OS-managed. Use LAN for
desktop-to-desktop unless the OS has already formed the P2P group and routing.
