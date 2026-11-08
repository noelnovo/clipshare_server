package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"clipshare/src/internal/config"
)

func TestDefaults(t *testing.T) {
	cfg := config.Default()

	if cfg.Server.Port != 40403 {
		t.Errorf("server port: got %d, want 40403", cfg.Server.Port)
	}
	if cfg.Server.Bind != "0.0.0.0" {
		t.Errorf("server bind: got %q, want 0.0.0.0", cfg.Server.Bind)
	}
	if cfg.API.Port != 40405 {
		t.Errorf("api port: got %d, want 40405", cfg.API.Port)
	}
	if cfg.API.Bind != "127.0.0.1" {
		t.Errorf("api bind: got %q, want 127.0.0.1", cfg.API.Bind)
	}
	if cfg.Discovery.BeaconPort != 40404 {
		t.Errorf("beacon port: got %d, want 40404", cfg.Discovery.BeaconPort)
	}
	if cfg.Connection.Mode != config.ModeDiscover {
		t.Errorf("connection mode: got %q, want %q", cfg.Connection.Mode, config.ModeDiscover)
	}
	if cfg.MaxImageBytes != 10*1024*1024 {
		t.Errorf("max_image_bytes: got %d, want 10MB", cfg.MaxImageBytes)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log level: got %q, want info", cfg.Log.Level)
	}
	if cfg.Clipboard.Backend != "auto" {
		t.Errorf("clipboard backend: got %q, want auto", cfg.Clipboard.Backend)
	}
	if !cfg.TLS.VerifyHostname {
		t.Error("TLS verify_hostname: got false, want true (strict by default)")
	}
	if !cfg.WiFiDirect.Enabled {
		t.Error("wifi_direct enabled: got false, want true")
	}
}

func TestPathUsesEnvVar(t *testing.T) {
	want := "/tmp/clipshare-test-config.toml"
	t.Setenv("CLIPSHARE_CONFIG", want)
	if got := config.Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathFallsBackToHome(t *testing.T) {
	t.Setenv("CLIPSHARE_CONFIG", "")
	t.Setenv("APPDATA", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "clipshare", "config.toml")
	if got := config.Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathFallsBackToWindowsUserProfile(t *testing.T) {
	t.Setenv("CLIPSHARE_CONFIG", "")
	t.Setenv("HOME", "")
	t.Setenv("APPDATA", "")
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	want := filepath.Join(home, ".config", "clipshare", "config.toml")
	if got := config.Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathUsesAppDataOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific config location")
	}
	t.Setenv("CLIPSHARE_CONFIG", "")
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	want := filepath.Join(`C:\Users\test\AppData\Roaming`, "clipshare", "config.toml")
	if got := config.Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLIPSHARE_CONFIG", filepath.Join(dir, "missing.toml"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed for missing file: %v", err)
	}
	if cfg.Server.Port != 40403 {
		t.Errorf("expected default server port, got %d", cfg.Server.Port)
	}
}

func TestLoadOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := `
device_name = "test-device"
token = "secret"

[server]
port = 50001
bind = "127.0.0.1"

[api]
port = 50002
bind = "0.0.0.0"

[connection]
mode = "whitelist"

[[connection.whitelist]]
name = "phone"
ip = "192.168.1.10"

[discovery]
mdns = false
beacon = false
beacon_port = 50004
beacon_addr = "192.168.1.255"

[log]
level = "debug"

[tls]
verify_hostname = false

[wifi_direct]
enabled = false
interfaces = ["p2p0"]
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("CLIPSHARE_CONFIG", path)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.DeviceName != "test-device" {
		t.Errorf("device_name: got %q, want test-device", cfg.DeviceName)
	}
	if cfg.Token != "secret" {
		t.Errorf("token: got %q, want secret", cfg.Token)
	}
	if cfg.Server.Port != 50001 {
		t.Errorf("server port: got %d, want 50001", cfg.Server.Port)
	}
	if cfg.API.Port != 50002 {
		t.Errorf("api port: got %d, want 50002", cfg.API.Port)
	}
	if cfg.Connection.Mode != config.ModeWhitelist {
		t.Errorf("mode: got %q, want whitelist", cfg.Connection.Mode)
	}
	if len(cfg.Connection.Whitelist) != 1 || cfg.Connection.Whitelist[0].Name != "phone" {
		t.Errorf("whitelist: got %+v", cfg.Connection.Whitelist)
	}
	if cfg.Discovery.MDNS {
		t.Error("expected mdns disabled")
	}
	if cfg.Discovery.Beacon {
		t.Error("expected beacon disabled")
	}
	if cfg.Discovery.BeaconPort != 50004 {
		t.Errorf("beacon port: got %d, want 50004", cfg.Discovery.BeaconPort)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log level: got %q, want debug", cfg.Log.Level)
	}
	if cfg.TLS.VerifyHostname {
		t.Error("TLS verify_hostname: got true, want false (parsed from toml)")
	}
	if cfg.WiFiDirect.Enabled {
		t.Error("wifi_direct enabled: got true, want false")
	}
	if len(cfg.WiFiDirect.Interfaces) != 1 || cfg.WiFiDirect.Interfaces[0] != "p2p0" {
		t.Errorf("wifi_direct interfaces: got %+v", cfg.WiFiDirect.Interfaces)
	}
}

func TestSaveAndLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLIPSHARE_CONFIG", filepath.Join(dir, "config.toml"))

	cfg := config.Default()
	cfg.DeviceName = "roundtrip"
	cfg.Token = "tok"
	cfg.Server.Port = 60001
	cfg.Connection.Mode = config.ModeWhitelist
	cfg.Connection.Whitelist = []config.WhitelistEntry{
		{Name: "a", IP: "10.0.0.1"},
	}
	cfg.TLS.VerifyHostname = false

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.DeviceName != cfg.DeviceName {
		t.Errorf("device_name: got %q, want %q", loaded.DeviceName, cfg.DeviceName)
	}
	if loaded.Token != cfg.Token {
		t.Errorf("token: got %q, want %q", loaded.Token, cfg.Token)
	}
	if loaded.Server.Port != cfg.Server.Port {
		t.Errorf("server port: got %d, want %d", loaded.Server.Port, cfg.Server.Port)
	}
	if loaded.Connection.Mode != cfg.Connection.Mode {
		t.Errorf("mode: got %q, want %q", loaded.Connection.Mode, cfg.Connection.Mode)
	}
	if len(loaded.Connection.Whitelist) != 1 || loaded.Connection.Whitelist[0].Name != "a" {
		t.Errorf("whitelist: got %+v", loaded.Connection.Whitelist)
	}
	if loaded.TLS.VerifyHostname {
		t.Error("verify_hostname: got true after roundtrip, want false")
	}
}

func TestBackwardCompatTopLevelMdns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := `
mdns = false
broadcast = 10
watch = 1000
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("CLIPSHARE_CONFIG", path)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Discovery.MDNS {
		t.Error("expected mdns to inherit top-level mdns=false")
	}
	if cfg.Broadcast != 10 {
		t.Errorf("broadcast: got %d, want 10", cfg.Broadcast)
	}
	if cfg.Watch != 1000 {
		t.Errorf("watch: got %d, want 1000", cfg.Watch)
	}
}

func TestInWhitelist(t *testing.T) {
	cfg := config.Default()
	cfg.Connection.Whitelist = []config.WhitelistEntry{
		{Name: "phone", IP: "192.168.1.10"},
		{Name: "tablet", IP: ""},
	}

	if !cfg.InWhitelist("192.168.1.10", "anything") {
		t.Error("expected IP match")
	}
	if !cfg.InWhitelist("", "tablet") {
		t.Error("expected name match")
	}
	if cfg.InWhitelist("192.168.1.99", "unknown") {
		t.Error("expected no match")
	}
}

func TestBroadcastInterval(t *testing.T) {
	cfg := config.Default()
	if cfg.BroadcastInterval() != 5*time.Second {
		t.Errorf("default interval: got %v, want 5s", cfg.BroadcastInterval())
	}
	cfg.Broadcast = 0
	if cfg.BroadcastInterval() != 5*time.Second {
		t.Errorf("zero interval fallback: got %v", cfg.BroadcastInterval())
	}
	cfg.Broadcast = 12
	if cfg.BroadcastInterval() != 12*time.Second {
		t.Errorf("interval: got %v, want 12s", cfg.BroadcastInterval())
	}
}

func TestWatchInterval(t *testing.T) {
	cfg := config.Default()
	if cfg.WatchInterval() != 300*time.Millisecond {
		t.Errorf("default interval: got %v", cfg.WatchInterval())
	}
	cfg.Watch = 40
	if cfg.WatchInterval() != 300*time.Millisecond {
		t.Errorf("low value fallback: got %v", cfg.WatchInterval())
	}
	cfg.Watch = 1000
	if cfg.WatchInterval() != 1000*time.Millisecond {
		t.Errorf("interval: got %v, want 1000ms", cfg.WatchInterval())
	}
}

func TestLoadNormalizesInvalidValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := `
[clipboard]
backend = "invalid"

[log]
level = "unknown"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("CLIPSHARE_CONFIG", path)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Clipboard.Backend != "auto" {
		t.Errorf("clipboard backend: got %q, want auto", cfg.Clipboard.Backend)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log level: got %q, want info", cfg.Log.Level)
	}
}
