package websocket

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"

	"clipshare/src/internal/certs"
	"clipshare/src/internal/clip"
	"clipshare/src/internal/config"
	"clipshare/src/internal/consts"
)

// GroupOwnerTLSConfig builds the mTLS client configuration used to dial a phone
// that owns the Wi-Fi Direct group. The phone's CA verifies the phone's server
// certificate, and the client certificate proves this PC to the phone.
//
// The phone's certificate is issued to a name, not to the group-owner IP, so the
// chain is verified against the CA and the dialed address is not checked.
func GroupOwnerTLSConfig(cfg *config.Config) (*tls.Config, error) {
	wd := cfg.WiFiDirect
	if wd.ClientCA == "" || wd.ClientCert == "" || wd.ClientKey == "" {
		return nil, fmt.Errorf("wifi_direct client role needs client_ca, client_cert and client_key")
	}
	cert, err := tls.LoadX509KeyPair(wd.ClientCert, wd.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("load client certificate: %w", err)
	}
	pool, err := certs.LoadPool(wd.ClientCA)
	if err != nil {
		return nil, fmt.Errorf("load phone CA: %w", err)
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            pool,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true,
		VerifyConnection:   verifyChain(pool),
	}, nil
}

// RunGroupOwner keeps a WebSocket connection to a phone group owner open at
// ownerIP:<server port>, reconnecting with backoff until ctx is done. Received
// clipboard content is passed to onRemote, the same as desktop peers.
func (s *Server) RunGroupOwner(ctx context.Context, ownerIP string, tlsCfg *tls.Config, version string, onRemote func(content clip.Content, from string)) {
	addr := ownerIP + ":" + strconv.Itoa(s.cfg.Server.Port)
	pc := &PeerClient{srv: s, onRemote: onRemote}
	pc.dialLoop(ctx, ownerIP, addr, consts.SchemeWSS, tlsCfg, version)
}
