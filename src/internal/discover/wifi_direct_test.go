package discover

import (
	"net"
	"testing"

	"clipshare/src/internal/config"
)

func TestMatchesWiFiDirectInterface(t *testing.T) {
	if !matchesWiFiDirectInterface("p2p-wlan0-0", nil) {
		t.Error("expected Linux P2P interface to match")
	}
	if !matchesWiFiDirectInterface("Wi-Fi Direct Virtual Adapter", nil) {
		t.Error("expected Windows P2P interface to match")
	}
	if matchesWiFiDirectInterface("eth0", nil) {
		t.Error("did not expect ordinary interface to match")
	}
	if !matchesWiFiDirectInterface("p2p0", []string{"p2p0"}) {
		t.Error("expected explicitly configured interface to match")
	}
	if matchesWiFiDirectInterface("p2p0", []string{"wlan0"}) {
		t.Error("did not expect non-configured interface to match")
	}
}

func TestMatchesWiFiDirectAddress(t *testing.T) {
	for _, value := range []string{"192.168.49.1", "192.168.137.1"} {
		if !matchesWiFiDirectAddress(net.ParseIP(value)) {
			t.Errorf("expected P2P address %s to match", value)
		}
	}
	if matchesWiFiDirectAddress(net.ParseIP("192.168.1.10")) {
		t.Error("did not expect ordinary LAN address to match")
	}
}

func TestSanitizeHost(t *testing.T) {
	if got := sanitizeHost("My Laptop!"); got != "my-laptop" {
		t.Errorf("sanitizeHost() = %q, want my-laptop", got)
	}
	if got := sanitizeHost(" "); got != "clipshare" {
		t.Errorf("sanitizeHost(blank) = %q, want clipshare", got)
	}
}

func TestInterfaceKeyIsStable(t *testing.T) {
	key := interfaceKey(nil, nil)
	if key != "" {
		t.Errorf("interfaceKey(nil, nil) = %q, want empty", key)
	}
}

func TestWiFiDirectRequiresTLS(t *testing.T) {
	cfg := config.Default()
	cfg.WiFiDirect.Enabled = true
	cfg.TLS.Enabled = false
	if wifiDirectAllowed(cfg) {
		t.Error("Wi-Fi Direct must be refused when TLS is disabled")
	}
	cfg.TLS.Enabled = true
	if !wifiDirectAllowed(cfg) {
		t.Error("Wi-Fi Direct should be allowed when TLS is enabled")
	}
	cfg.WiFiDirect.Enabled = false
	if wifiDirectAllowed(cfg) {
		t.Error("Wi-Fi Direct should be refused when disabled")
	}
}

func TestStartPlatformWiFiDirectDisabled(t *testing.T) {
	cfg := &config.Config{WiFiDirect: config.WiFiDirect{Enabled: false}}
	if err := startPlatformWiFiDirect(t.Context(), cfg); err != nil {
		t.Errorf("startPlatformWiFiDirect(disabled) = %v, want nil", err)
	}
}

func TestStartWhitelistModeDoesNotAdvertise(t *testing.T) {
	cfg := config.Default()
	cfg.Connection.Mode = config.ModeWhitelist
	cfg.WiFiDirect.Enabled = false
	if err := Start(t.Context(), cfg); err != nil {
		t.Errorf("Start(whitelist) = %v, want nil", err)
	}
}
