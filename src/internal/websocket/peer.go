package websocket

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"clipshare/src/internal/clip"
	"clipshare/src/internal/consts"
	"clipshare/src/internal/log"
	"clipshare/src/internal/protocol"
)

// PeerClient connects out to a remote clipshare daemon (desktop-to-desktop).
type PeerClient struct {
	srv      *Server
	conn     *websocket.Conn
	connMu   sync.Mutex
	onRemote func(content clip.Content, from string)
}

// RunPeers dials each configured peer and reconnects with backoff until ctx
// is done. onRemote receives clipboard content pushed by the remote daemon.
func (s *Server) RunPeers(ctx context.Context, version string, onRemote func(content clip.Content, from string)) {
	for _, host := range s.cfg.Peers {
		host := strings.TrimSpace(host)
		if host == "" {
			continue
		}
		pc := &PeerClient{srv: s, onRemote: onRemote}
		go pc.run(ctx, host, version)
	}
}

func (pc *PeerClient) run(ctx context.Context, host, version string) {
	addr := host
	if !strings.Contains(addr, ":") {
		addr += ":" + strconv.Itoa(pc.srv.cfg.Server.Port)
	}
	scheme := consts.SchemeWS
	var tlsCfg *tls.Config
	if pc.srv.cfg.TLS.Enabled {
		scheme = consts.SchemeWSS
		cfg, err := pc.srv.peerTLSConfig()
		if err != nil {
			log.Errorf("peer %s: tls config: %v", host, err)
			return
		}
		tlsCfg = cfg
	}
	pc.dialLoop(ctx, host, addr, scheme, tlsCfg, version)
}

// dialLoop keeps a connection to addr open, reconnecting with backoff, until
// ctx is done. tlsCfg is used only for wss.
func (pc *PeerClient) dialLoop(ctx context.Context, host, addr, scheme string, tlsCfg *tls.Config, version string) {
	var opts *websocket.DialOptions
	if tlsCfg != nil {
		opts = &websocket.DialOptions{
			HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}},
		}
	}
	u := url.URL{Scheme: scheme, Host: addr, Path: consts.WSPath}
	if pc.srv.cfg.Token != "" {
		q := u.Query()
		q.Set("token", pc.srv.cfg.Token)
		u.RawQuery = q.Encode()
	}
	backoff := pc.srv.cfg.Timing.PeerBackoffInitial
	for {
		conn, _, err := websocket.Dial(ctx, u.String(), opts)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Debugf("peer %s: dial failed: %v (retry in %s)", host, err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < pc.srv.cfg.Timing.PeerBackoffMax {
				backoff *= 2
			}
			continue
		}
		conn.SetReadLimit(pc.srv.cfg.MaxMessageBytes)
		backoff = pc.srv.cfg.Timing.PeerBackoffInitial
		pc.connMu.Lock()
		pc.conn = conn
		pc.connMu.Unlock()
		pc.srv.registerPeer(pc)
		log.Infof("peer %s: connected", host)

		hello, _ := json.Marshal(protocol.Envelope{Type: protocol.MsgHello, Data: protocol.MustJSON(protocol.Hello{
			Name: pc.srv.cfg.DeviceName, Platform: protocol.PlatformDesktop, Version: version,
		})})
		if err := conn.Write(ctx, websocket.MessageText, hello); err != nil {
			pc.close()
			pc.srv.unregisterPeer(pc)
			continue
		}

		go pc.keepalive(ctx, conn)
		pc.readLoop(ctx, conn, host)
		pc.close()
		pc.srv.unregisterPeer(pc)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (pc *PeerClient) readLoop(ctx context.Context, conn *websocket.Conn, host string) {
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			log.Debugf("peer %s: read: %v", host, err)
			return
		}
		var env protocol.Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}
		if env.Type != protocol.MsgClipboard {
			continue
		}
		var cm protocol.ClipboardMsg
		if err := json.Unmarshal(env.Data, &cm); err != nil {
			continue
		}
		content, ok := protocol.MsgToContent(cm)
		if !ok || pc.onRemote == nil {
			continue
		}
		pc.onRemote(content, cm.From)
	}
}

func (pc *PeerClient) keepalive(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(pc.srv.cfg.Timing.PeerKeepalive)
	defer t.Stop()
	ping, _ := json.Marshal(protocol.Envelope{Type: protocol.MsgPing})
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			conn.Write(ctx, websocket.MessageText, ping)
		}
	}
}

// Send pushes clipboard content to the connected peer, if any.
func (pc *PeerClient) Send(content clip.Content, from string) {
	pc.connMu.Lock()
	conn := pc.conn
	pc.connMu.Unlock()
	if conn == nil {
		return
	}
	payload, _ := json.Marshal(protocol.ContentMsg(content, protocol.NowMillis(), from))
	msg, _ := json.Marshal(protocol.Envelope{Type: protocol.MsgClipboard, Data: payload})
	ctx, cancel := context.WithTimeout(context.Background(), pc.srv.cfg.Timing.WriteTimeout)
	defer cancel()
	conn.Write(ctx, websocket.MessageText, msg)
}

func (pc *PeerClient) close() {
	pc.connMu.Lock()
	defer pc.connMu.Unlock()
	if pc.conn != nil {
		pc.conn.Close(websocket.StatusNormalClosure, "bye")
		pc.conn = nil
	}
}
