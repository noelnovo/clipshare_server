package discover

import (
	"context"
	"fmt"

	"clipshare/src/internal/config"
	"clipshare/src/internal/log"
)

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
	if err := startWiFiDirect(ctx, cfg); err != nil {
		return fmt.Errorf("wifi direct advertise: %w", err)
	}

	go func() {
		if err := beaconIfEnabled(ctx, cfg); err != nil {
			log.Errorf("udp beacon failed: %v", err)
		}
	}()

	return nil
}
