package websocket

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"clipshare/src/internal/clip"
	"clipshare/src/internal/config"
	"clipshare/src/internal/consts"
	"clipshare/src/internal/log"
	"clipshare/src/internal/protocol"
)

// Server accepts WebSocket peers and brokers clipboard messages.
type Server struct {
	cfg             *config.Config
	version         string
	clients         map[string]*Client
	peers           map[*PeerClient]struct{}
	mu              sync.Mutex
	maxClients      int
	onRemoteClip    func(content clip.Content, from string)
	onClientConnect func()
	lastBroadcast   uint64
	lastBroadcastT  time.Time
	lastRemoteKey   uint64
	lastRemoteAt    time.Time
	started         time.Time
}

// New creates a new WebSocket server.
func New(cfg *config.Config, version string, onRemote func(content clip.Content, from string)) *Server {
	return &Server{
		cfg:          cfg,
		version:      version,
		clients:      make(map[string]*Client),
		peers:        make(map[*PeerClient]struct{}),
		onRemoteClip: onRemote,
		started:      time.Now(),
	}
}

// SetConnectHook registers a callback fired after a client completes its
// hello handshake. Used by the one-shot share mode.
func (s *Server) SetConnectHook(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClientConnect = fn
}

// SetMaxClients limits how many WebSocket clients the server keeps at once.
// Additional connections are closed immediately. 0 means unlimited. Transient
// modes (watch, one-shot send) use 1 so a client that bursts several parallel
// connections only holds one.
func (s *Server) SetMaxClients(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxClients = n
}

// SetOnRemoteClip sets the handler for remote clipboard content. It allows the
// handler to be wired after the watcher exists.
func (s *Server) SetOnRemoteClip(fn func(content clip.Content, from string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onRemoteClip = fn
}

func (s *Server) registerPeer(pc *PeerClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[pc] = struct{}{}
}

func (s *Server) unregisterPeer(pc *PeerClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.peers, pc)
}

// ListenAndServe starts the WebSocket listener and blocks until ctx is done.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Bind, s.cfg.Server.Port)
	mux := http.NewServeMux()
	mux.HandleFunc(consts.WSPath, s.handleWS)
	srv := &http.Server{Handler: mux}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	scheme := consts.SchemeWS
	if s.cfg.TLS.Enabled {
		tlsCfg, err := s.tlsConfig()
		if err != nil {
			ln.Close()
			return err
		}
		ln = tlsListener(ln, tlsCfg)
		scheme = consts.SchemeWSS
	}
	go func() {
		<-ctx.Done()
		srv.Close()
		ln.Close()
	}()
	log.Infof("%s server listening on %s", scheme, addr)
	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// Clients returns a snapshot of connected clients.
func (s *Server) Clients() []protocol.ClientInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]protocol.ClientInfo, 0, len(s.clients))
	for _, c := range s.clients {
		out = append(out, protocol.ClientInfo{
			ID: c.id, Name: c.name, Platform: c.platform, Version: c.version, IP: c.ip,
		})
	}
	return out
}

// Uptime returns how long the server has been running.
func (s *Server) Uptime() time.Duration { return time.Since(s.started) }

// DeviceName returns the configured device name.
func (s *Server) DeviceName() string { return s.cfg.DeviceName }

// TokenAuth reports whether token authentication is enabled.
func (s *Server) TokenAuth() bool { return s.cfg.Token != "" }

// TLS reports whether the server uses TLS.
func (s *Server) TLS() bool { return s.cfg.TLS.Enabled }

// Mode returns the configured connection mode.
func (s *Server) Mode() string { return s.cfg.Connection.Mode }
