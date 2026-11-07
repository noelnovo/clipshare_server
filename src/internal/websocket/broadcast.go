package websocket

import (
	"encoding/json"
	"time"

	"clipshare/src/internal/clip"
	"clipshare/src/internal/log"
	"clipshare/src/internal/protocol"
)

// Broadcast sends clipboard content to every connected client except the
// origin (skipID "" means broadcast to all).
func (s *Server) Broadcast(content clip.Content, from, skipID string) {
	if content.Kind == clip.KindImage && int64(len(content.Image)) > s.cfg.MaxImageBytes {
		log.Warnf("dropping image broadcast: %d bytes exceeds max_image_bytes=%d",
			len(content.Image), s.cfg.MaxImageBytes)
		return
	}
	payload, _ := json.Marshal(protocol.ContentMsg(content, protocol.NowMillis(), from))
	msg, _ := json.Marshal(protocol.Envelope{Type: protocol.MsgClipboard, Data: payload})
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.clients {
		if skipID != "" && id == skipID {
			continue
		}
		if err := c.write(msg); err != nil {
			log.Errorf("broadcast to %s: %v", id, err)
		}
	}
	for pc := range s.peers {
		pc.Send(content, from)
	}
}

// BroadcastLocal fans out a local clipboard change to all connected clients.
// Clients that sent us this exact content within the echo window are skipped,
// so a watcher misfire can never bounce a client's own clip back to it.
func (s *Server) BroadcastLocal(content clip.Content, from string) {
	key := protocol.ContentKey(content)
	window := s.cfg.Timing.EchoWindow
	payload, _ := json.Marshal(protocol.ContentMsg(content, protocol.NowMillis(), from))
	msg, _ := json.Marshal(protocol.Envelope{Type: protocol.MsgClipboard, Data: payload})
	s.mu.Lock()
	defer s.mu.Unlock()
	// Record this as our own broadcast so echoes coming back from peers are
	// recognized and dropped instead of being written to the local clipboard.
	s.lastBroadcast = key
	s.lastBroadcastT = time.Now()
	for id, c := range s.clients {
		if c.recentlySent(key, window) {
			log.Debugf("broadcast to %s skipped: same content received recently", id)
			continue
		}
		if err := c.write(msg); err != nil {
			log.Errorf("broadcast to %s: %v", id, err)
		}
	}
	for pc := range s.peers {
		pc.Send(content, from)
	}
}

// broadcastRemote forwards content received from a remote device to all other
// connected devices, never back to the origin device.
func (s *Server) broadcastRemote(content clip.Content, originDevice string, originClient *Client, originPeer *PeerClient) {
	if content.Kind == clip.KindImage && int64(len(content.Image)) > s.cfg.MaxImageBytes {
		log.Warnf("dropping image broadcast: %d bytes exceeds max_image_bytes=%d",
			len(content.Image), s.cfg.MaxImageBytes)
		return
	}
	payload, _ := json.Marshal(protocol.ContentMsg(content, protocol.NowMillis(), originDevice))
	msg, _ := json.Marshal(protocol.Envelope{Type: protocol.MsgClipboard, Data: payload})
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.clients {
		if originClient != nil && id == originClient.id {
			continue
		}
		if c.name == originDevice {
			continue
		}
		if err := c.write(msg); err != nil {
			log.Errorf("broadcast to %s: %v", id, err)
		}
	}
	for pc := range s.peers {
		if pc == originPeer {
			continue
		}
		if pc.remoteDevice() == originDevice {
			continue
		}
		pc.Send(content, originDevice)
	}
}

func (s *Server) sendError(c *Client, code, msg string) {
	payload, _ := json.Marshal(protocol.ErrorMsg{Code: code, Msg: msg})
	m, _ := json.Marshal(protocol.Envelope{Type: protocol.MsgError, Data: payload})
	if err := c.write(m); err != nil {
		log.Errorf("error send: %v", err)
	}
}

func (s *Server) pong(c *Client) {
	m, _ := json.Marshal(protocol.Envelope{Type: protocol.MsgPong})
	c.write(m)
}
