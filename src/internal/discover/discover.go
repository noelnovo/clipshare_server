package discover

import (
	"context"
	"fmt"

	"clipshare/src/internal/config"
	"clipshare/src/internal/log"
)

// RunWiFiDirectClient joins the phone group owner named by
// wifi_direct.peer_name and calls onOwner with its IP each time the link is up.
// It does nothing unless Wi-Fi Direct is enabled with the client role.
func RunWiFiDirectClient(ctx context.Context, cfg *config.Config, onOwner func(ownerIP string)) {
	if !cfg.WiFiDirect.Enabled || cfg.WiFiDirect.Role != config.WiFiDirectRoleClient {
		return
	}
	if cfg.WiFiDirect.PeerName == "" {
		log.Warnf("wifi direct client: wifi_direct.peer_name is not set")
		return
	}
	go runPlatformWiFiDirectClient(ctx, cfg.WiFiDirect.PeerName, onOwner)
}

// Start begins LAN discovery (mDNS + UDP beacon) when the connection mode is
// "discover". It returns immediately and runs the enabled methods in the
// background until ctx is cancelled. An error is returned only when a enabled
// method fails to start; failures after startup are logged.
func Start(ctx context.Context, cfg *config.Config) error {
	if cfg.Connection.Mode != config.ModeDiscover {
		log.Infof("connection mode %q: not advertising (whitelist-only)", cfg.Connection.Mode)
		return nil
	}

	if err := advertiseIfEnabled(ctx, cfg); err != nil {
		return fmt.Errorf("mdns advertise: %w", err)
	}
	// The client role joins a phone-owned group, so this PC must not also
	// become a group owner or advertise on the P2P interface.
	if cfg.WiFiDirect.Role != config.WiFiDirectRoleClient {
		if err := startPlatformWiFiDirect(ctx, cfg); err != nil {
			return fmt.Errorf("wifi direct platform startup: %w", err)
		}
		if err := startWiFiDirect(ctx, cfg); err != nil {
			return fmt.Errorf("wifi direct advertise: %w", err)
		}
	}

	go func() {
		if err := beaconIfEnabled(ctx, cfg); err != nil {
			log.Errorf("udp beacon failed: %v", err)
		}
	}()

	return nil
}
