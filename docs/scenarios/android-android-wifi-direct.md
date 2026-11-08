# Android Server ↔ Android Client over Wi-Fi Direct

No desktop involved. One phone must be Server, the other Client.

## With mTLS

1. Server phone: **Server** mode, **Wi-Fi Direct** on, **Server TLS** on.
2. Client phone: **Client** mode, **Wi-Fi Direct** on, **TLS (wss)** on.
3. Server phone: **Settings → Server → Show client secret**.
4. Client phone: **Settings → Client → Scan client secret**.

Each QR is a fresh client key/certificate from that server's CA. Repeat per
server CA.

## Plaintext LAN test only

Both TLS switches off, same LAN, default port `40403`. Do not use Wi-Fi
Direct plaintext with Android 1.1.6+; the client refuses it.
