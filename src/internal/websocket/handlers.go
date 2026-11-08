package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"clipshare/src/internal/config"
	"clipshare/src/internal/log"
	"clipshare/src/internal/protocol"
)

const remoteDedupWindow = 2 * time.Second

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Token != "" && r.URL.Query().Get("token") != s.cfg.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Errorf("ws accept: %v", err)
		return
	}
	c.SetReadLimit(s.cfg.MaxMessageBytes)
	client := &Client{id: newID(), conn: c, ip: clientIP(r.RemoteAddr), cfg: s.cfg}
	s.mu.Lock()
	if s.maxClients > 0 && len(s.clients) >= s.maxClients {
		s.mu.Unlock()
		log.Debugf("rejecting extra connection from %s: server accepts one client", client.ip)
		c.Close(websocket.StatusPolicyViolation, "one client at a time")
		return
	}
	s.clients[client.id] = client
	s.mu.Unlock()
	log.Infof("client connected: %s (%s)", client.id, client.ip)
	s.handleClient(r.Context(), client)
}

func (s *Server) handleClient(ctx context.Context, c *Client) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, c.id)
		s.mu.Unlock()
		c.conn.Close(websocket.StatusNormalClosure, "bye")
		log.Infof("client disconnected: %s", c.id)
	}()

	readCtx, cancel := context.WithTimeout(ctx, s.cfg.Timing.HelloTimeout)
	defer cancel()
	_, msg, err := c.conn.Read(readCtx)
	if err != nil {
		return
	}
	var env protocol.Envelope
	if err := json.Unmarshal(msg, &env); err != nil || env.Type != protocol.MsgHello {
		s.sendError(c, protocol.ErrBadHello, "expected hello message")
		return
	}
	var hello protocol.Hello
	if err := json.Unmarshal(env.Data, &hello); err != nil {
		s.sendError(c, protocol.ErrBadHello, "invalid hello payload")
		return
	}

	// Whitelist mode: only devices matching the whitelist by IP or name are
	// allowed to talk to us (in addition to token/TLS checks).
	if s.cfg.Connection.Mode == config.ModeWhitelist && !s.cfg.InWhitelist(c.ip, hello.Name) {
		s.sendError(c, "not_whitelisted", "device not in whitelist")
		log.Warnf("rejected non-whitelisted hello from %q (%s)", hello.Name, c.ip)
		return
	}

	c.name = hello.Name
	c.platform = hello.Platform
	c.version = hello.Version
	log.Infof("hello from %q (%s v%s)", hello.Name, hello.Platform, hello.Version)

	s.mu.Lock()
	hook := s.onClientConnect
	s.mu.Unlock()
	if hook != nil {
		hook()
	}

	// Reply with our own identity so clients can show the connected device name.
	reply, _ := json.Marshal(protocol.Envelope{Type: protocol.MsgHello, Data: protocol.MustJSON(protocol.Hello{
		Name: s.cfg.DeviceName, Platform: protocol.PlatformDesktop, Version: s.version,
	})})
	c.write(reply)

	for {
		_, msg, err := c.conn.Read(context.Background())
		if err != nil {
			return
		}
		var env protocol.Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			s.sendError(c, protocol.ErrBadJSON, "malformed message")
			continue
		}
		switch env.Type {
		case protocol.MsgPing:
			s.pong(c)
		case protocol.MsgPong:
		case protocol.MsgClipboard:
			var cm protocol.ClipboardMsg
			if err := json.Unmarshal(env.Data, &cm); err != nil {
				s.sendError(c, protocol.ErrBadClipboard, "invalid clipboard payload")
				continue
			}
			s.ReceiveClipboard(c, nil, cm)
		default:
			s.sendError(c, protocol.ErrUnknownType, "unrecognized message type")
		}
	}
}

// ReceiveClipboard is the single entry point for clipboard content arriving
// from any transport (inbound WebSocket client or outbound peer read loop).
// originClient is set for inbound clients; originPeer for peer connections.
func (s *Server) ReceiveClipboard(originClient *Client, originPeer *PeerClient, msg protocol.ClipboardMsg) {
	content, ok := protocol.MsgToContent(msg)
	if !ok {
		return
	}
	key := protocol.ContentKey(content)
	if originClient != nil {
		originClient.noteIncoming(key)
	}

	// Loop protection: ignore content that is an echo of something we
	// broadcast ourselves, and ignore duplicate deliveries of the same content
	// that arrive over multiple connections to the same peer.
	s.mu.Lock()
	isEcho := key == s.lastBroadcast && time.Since(s.lastBroadcastT) < s.cfg.Timing.EchoWindow
	isDup := key == s.lastRemoteKey && time.Since(s.lastRemoteAt) < remoteDedupWindow
	if !isEcho && !isDup {
		s.lastRemoteKey = key
		s.lastRemoteAt = time.Now()
	}
	// NOTE: deliberately do NOT update s.lastBroadcast here. lastBroadcast is
	// only set by BroadcastLocal when we ourselves originate content, so that
	// echoes coming back from peers can be recognized and dropped. Treating
	// received content as our own broadcast caused legitimate re-deliveries
	// (and re-copies of the same text) to be dropped as false echoes.
	s.mu.Unlock()
	if isEcho || isDup {
		log.Debugf("clipboard from %q ignored (echo=%v dup=%v)", msg.From, isEcho, isDup)
		return
	}
	log.Debugf("clipboard received from %q", msg.From)
	if s.onRemoteClip != nil {
		s.onRemoteClip(content, msg.From)
	}
	s.broadcastRemote(content, msg.From, originClient, originPeer)
}

// handleClipboard is retained for callers/tests that use the old name.
func (s *Server) handleClipboard(from *Client, msg protocol.ClipboardMsg) {
	s.ReceiveClipboard(from, nil, msg)
}
