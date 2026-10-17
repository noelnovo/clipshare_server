//go:build !windows || !amd64

package discover

import "context"

// runPlatformWiFiDirectClient is unsupported outside Windows amd64: the phone
// group-owner client needs the WinRT Wi-Fi Direct API.
func runPlatformWiFiDirectClient(ctx context.Context, peerName string, onOwner func(ownerIP string)) {}
