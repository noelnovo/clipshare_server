//go:build !windows || !amd64

package discover

import (
	"context"

	"clipshare/src/internal/config"
)

func startPlatformWiFiDirect(context.Context, *config.Config) error {
	return nil
}
