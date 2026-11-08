package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DeviceName      string     `toml:"device_name"`
	Server          Server     `toml:"server"`
	API             API        `toml:"api"`
	Mdns            bool       `toml:"mdns"`      // deprecated: prefer Discovery.MDNS
	Broadcast       int        `toml:"broadcast"` // beacon interval in seconds
	Watch           int        `toml:"watch"`
	Token           string     `toml:"token"`
	Peers           []string   `toml:"peers"`
	Connection      Connection `toml:"connection"`
	TLS             TLS        `toml:"tls"`
	MaxImageBytes   int64      `toml:"max_image_bytes"`
	MaxMessageBytes int64      `toml:"max_message_bytes"`
	Discovery       Discovery  `toml:"discovery"`
	WiFiDirect      WiFiDirect `toml:"wifi_direct"`
	Timing          Timing     `toml:"timing"`
	Clipboard       Clipboard  `toml:"clipboard"`
	Log             Log        `toml:"log"`
}

type Server struct {
	Port int    `toml:"port"`
	Bind string `toml:"bind"`
}

type API struct {
	Port int    `toml:"port"`
	Bind string `toml:"bind"`
}

// Connection governs how devices find and trust each other. The same options
// (mode + whitelist) exist on the Android app.
type Connection struct {
	Mode      string           `toml:"mode"` // "discover" | "whitelist"
	Whitelist []WhitelistEntry `toml:"whitelist"`
}

type WhitelistEntry struct {
	Name string `toml:"name"`
	IP   string `toml:"ip"`
}

// Discovery controls service advertisement on the LAN.
type Discovery struct {
	MDNS       bool   `toml:"mdns"`
	Beacon     bool   `toml:"beacon"`
	BeaconPort int    `toml:"beacon_port"`
	BeaconAddr string `toml:"beacon_addr"`
	// MDNSSubnets limits mDNS advertising to interfaces with an IPv4 address in
	// one of these CIDR ranges (for example "192.168.1.0/24"). Empty means all.
	MDNSSubnets []string `toml:"mdns_subnets"`
}

// WiFiDirect controls advertisement on an already-formed Wi-Fi Direct link.
// Group creation remains platform-owned; the daemon only discovers the P2P
// interface and advertises its existing WebSocket endpoint there.
type WiFiDirect struct {
	Enabled    bool     `toml:"enabled"`
	Interfaces []string `toml:"interfaces"`
	// Role is "host" (default: this PC is the Wi-Fi Direct group owner and
	// advertises itself) or "client" (this PC joins a phone-owned group and
	// dials the phone's WebSocket server over mTLS).
	Role string `toml:"role"`
	// PeerName is the Wi-Fi Direct device name of the phone group owner
	// (client role only), for example "Pixel 9".
	PeerName string `toml:"peer_name"`
	// ClientCA, ClientCert and ClientKey are the phone's CA and a client
	// certificate issued by it (client role only, PEM paths).
	ClientCA   string `toml:"client_ca"`
	ClientCert string `toml:"client_cert"`
	ClientKey  string `toml:"client_key"`
}

// WiFiDirectRoleHost and WiFiDirectRoleClient are the valid wifi_direct.role values.
const (
	WiFiDirectRoleHost   = "host"
	WiFiDirectRoleClient = "client"
)

// Clipboard selects the clipboard backend on Linux. Windows ignores this
// section because it uses the system clipboard API directly.
type Clipboard struct {
	Backend string `toml:"backend"` // "auto", "wayland", "xclip", "xsel"
}

// Timing holds user-tunable timeouts and intervals.
type Timing struct {
	HelloTimeout       time.Duration `toml:"hello_timeout"`
	WriteTimeout       time.Duration `toml:"write_timeout"`
	PeerKeepalive      time.Duration `toml:"peer_keepalive"`
	PeerBackoffInitial time.Duration `toml:"peer_backoff_initial"`
	PeerBackoffMax     time.Duration `toml:"peer_backoff_max"`
	EchoWindow         time.Duration `toml:"echo_window"`
	OneShotTimeout     time.Duration `toml:"one_shot_timeout"`
	OneShotGrace       time.Duration `toml:"one_shot_grace"`
	WatchRemoteTimeout time.Duration `toml:"watch_remote_timeout"`
}

// Log controls the daemon's log output.
type Log struct {
	Level string `toml:"level"` // "debug" | "info" | "warn" | "error"
	File  string `toml:"file"`  // empty = stderr
}

// TLS enables mutual TLS. CA is the shared trust root; Cert/Key are this
// device's identity (a server cert). VerifyHostname controls whether outbound
// peer dials require the server certificate to match the dialed host/IP SAN.
// It defaults to true (strict); set false to make certificates network
// independent (trust anchored on the CA alone).
type TLS struct {
	Enabled        bool   `toml:"enabled"`
	CA             string `toml:"ca"`
	Cert           string `toml:"cert"`
	Key            string `toml:"key"`
	VerifyHostname bool   `toml:"verify_hostname"`
}

const (
	ModeDiscover  = "discover"
	ModeWhitelist = "whitelist"
)

func Default() *Config {
	host, _ := os.Hostname()
	certsDir := filepath.Join(filepath.Dir(Path()), "certs")
	return &Config{
		DeviceName: host,
		Server:     Server{Port: 40403, Bind: "0.0.0.0"},
		API:        API{Port: 40405, Bind: "127.0.0.1"},
		Mdns:       true,
		Broadcast:  5,
		Watch:      300,
		Token:      "",
		Peers:      []string{},
		Connection: Connection{Mode: ModeDiscover},
		TLS: TLS{
			Enabled:        false,
			CA:             filepath.Join(certsDir, "ca.pem"),
			Cert:           filepath.Join(certsDir, "server.pem"),
			Key:            filepath.Join(certsDir, "server.key"),
			VerifyHostname: true,
		},
		MaxImageBytes:   10 * 1024 * 1024,
		MaxMessageBytes: 10 * 1024 * 1024,
		Discovery: Discovery{
			MDNS:       true,
			Beacon:     true,
			BeaconPort: 40404,
			BeaconAddr: "255.255.255.255",
		},
		WiFiDirect: WiFiDirect{Enabled: true, Role: WiFiDirectRoleHost},
		Clipboard:  Clipboard{Backend: "auto"},
		Log:        Log{Level: "info"},
		Timing: Timing{
			HelloTimeout:       5 * time.Second,
			WriteTimeout:       3 * time.Second,
			PeerKeepalive:      30 * time.Second,
			PeerBackoffInitial: 1 * time.Second,
			PeerBackoffMax:     30 * time.Second,
			EchoWindow:         30 * time.Second,
			OneShotTimeout:     120 * time.Second,
			OneShotGrace:       5 * time.Second,
			WatchRemoteTimeout: 120 * time.Second,
		},
	}
}

func Path() string {
	if cfg := os.Getenv("CLIPSHARE_CONFIG"); cfg != "" {
		return cfg
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "clipshare", "config.toml")
		}
	}

	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	if home == "" {
		home = os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
	}
	if home == "" {
		return filepath.Join(".config", "clipshare", "config.toml")
	}
	return filepath.Join(home, ".config", "clipshare", "config.toml")
}

func Load() (*Config, error) {
	cfg := Default()
	meta, err := toml.DecodeFile(Path(), cfg)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Backward compatibility: top-level mdns/broadcast are deprecated in favor
	// of [discovery] and [timing]. Apply them only when the new keys are absent.
	if !meta.IsDefined("discovery", "mdns") {
		cfg.Discovery.MDNS = cfg.Mdns
	}
	if !meta.IsDefined("discovery", "beacon") {
		cfg.Discovery.Beacon = true
	}
	if !meta.IsDefined("discovery", "beacon_port") {
		cfg.Discovery.BeaconPort = 40404
	}
	if !meta.IsDefined("discovery", "beacon_addr") {
		cfg.Discovery.BeaconAddr = "255.255.255.255"
	}
	if cfg.Discovery.BeaconPort <= 0 {
		cfg.Discovery.BeaconPort = 40404
	}
	if cfg.Discovery.BeaconAddr == "" {
		cfg.Discovery.BeaconAddr = "255.255.255.255"
	}
	cfg.Clipboard.Backend = normalizeClipboardBackend(cfg.Clipboard.Backend)
	cfg.WiFiDirect.Interfaces = normalizeInterfaces(cfg.WiFiDirect.Interfaces)
	if cfg.WiFiDirect.Role != WiFiDirectRoleClient {
		cfg.WiFiDirect.Role = WiFiDirectRoleHost
	}
	cfg.Log.Level = normalizeLogLevel(cfg.Log.Level)

	// Keep the deprecated top-level field in sync so re-saving is consistent.
	cfg.Mdns = cfg.Discovery.MDNS

	if cfg.Watch < 50 {
		cfg.Watch = 300
	}
	if cfg.Connection.Mode == "" {
		cfg.Connection.Mode = ModeDiscover
	}
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = 10 * 1024 * 1024
	}
	if cfg.MaxMessageBytes <= 0 {
		cfg.MaxMessageBytes = 10 * 1024 * 1024
	}
	if cfg.Server.Bind == "" {
		cfg.Server.Bind = "0.0.0.0"
	}
	if cfg.API.Bind == "" {
		cfg.API.Bind = "127.0.0.1"
	}
	cfg.Timing = cfg.Timing.withDefaults()
	return cfg, nil
}

func (t Timing) withDefaults() Timing {
	if t.HelloTimeout <= 0 {
		t.HelloTimeout = 5 * time.Second
	}
	if t.WriteTimeout <= 0 {
		t.WriteTimeout = 3 * time.Second
	}
	if t.PeerKeepalive <= 0 {
		t.PeerKeepalive = 30 * time.Second
	}
	if t.PeerBackoffInitial <= 0 {
		t.PeerBackoffInitial = 1 * time.Second
	}
	if t.PeerBackoffMax <= 0 {
		t.PeerBackoffMax = 30 * time.Second
	}
	if t.EchoWindow <= 0 {
		t.EchoWindow = 30 * time.Second
	}
	if t.OneShotTimeout <= 0 {
		t.OneShotTimeout = 120 * time.Second
	}
	if t.OneShotGrace <= 0 {
		t.OneShotGrace = 5 * time.Second
	}
	if t.WatchRemoteTimeout <= 0 {
		t.WatchRemoteTimeout = 120 * time.Second
	}
	return t
}

func (c *Config) Save() error {
	dir := filepath.Dir(Path())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return writeToml(c)
}

// InWhitelist reports whether a device is allowed to connect when in whitelist
// mode. It matches by IP or name (the name each device declares in hello).
func (c *Config) InWhitelist(ip, name string) bool {
	for _, e := range c.Connection.Whitelist {
		if e.IP != "" && e.IP == ip {
			return true
		}
		if e.Name != "" && e.Name == name {
			return true
		}
	}
	return false
}

func writeToml(c *Config) error {
	data := fmt.Sprintf(`device_name = %q
mdns = %v
broadcast = %d
watch = %d
token = %q
peers = %s
max_image_bytes = %d
max_message_bytes = %d

[connection]
mode = %q
whitelist = %s

[server]
port = %d
bind = %q

[api]
port = %d
bind = %q

[discovery]
mdns = %v
beacon = %v
beacon_port = %d
beacon_addr = %q
mdns_subnets = %s

[wifi_direct]
enabled = %v
interfaces = %s
role = %q
peer_name = %q
client_ca = %q
client_cert = %q
client_key = %q

[clipboard]
backend = %q

[log]
level = %q
file = %q

[timing]
hello_timeout = %q
write_timeout = %q
peer_keepalive = %q
peer_backoff_initial = %q
peer_backoff_max = %q
echo_window = %q
one_shot_timeout = %q
one_shot_grace = %q
watch_remote_timeout = %q

[tls]
enabled = %v
ca = %q
cert = %q
key = %q
verify_hostname = %v
`, c.DeviceName, c.Mdns, c.Broadcast, c.Watch, c.Token, tomlSlice(c.Peers),
		c.MaxImageBytes, c.MaxMessageBytes, c.Connection.Mode, tomlWhitelist(c.Connection.Whitelist),
		c.Server.Port, c.Server.Bind,
		c.API.Port, c.API.Bind,
		c.Discovery.MDNS, c.Discovery.Beacon, c.Discovery.BeaconPort, c.Discovery.BeaconAddr, tomlSlice(c.Discovery.MDNSSubnets),
		c.WiFiDirect.Enabled, tomlSlice(c.WiFiDirect.Interfaces),
		c.WiFiDirect.Role, c.WiFiDirect.PeerName, c.WiFiDirect.ClientCA, c.WiFiDirect.ClientCert, c.WiFiDirect.ClientKey,
		c.Clipboard.Backend,
		c.Log.Level, c.Log.File,
		c.Timing.HelloTimeout, c.Timing.WriteTimeout, c.Timing.PeerKeepalive,
		c.Timing.PeerBackoffInitial, c.Timing.PeerBackoffMax, c.Timing.EchoWindow,
		c.Timing.OneShotTimeout, c.Timing.OneShotGrace, c.Timing.WatchRemoteTimeout,
		c.TLS.Enabled, c.TLS.CA, c.TLS.Cert, c.TLS.Key, c.TLS.VerifyHostname)

	f, err := os.Create(Path())
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		return err
	}
	return f.Sync()
}

func tomlSlice(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	out := "["
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%q", v)
	}
	return out + "]"
}

func tomlWhitelist(w []WhitelistEntry) string {
	if len(w) == 0 {
		return "[]"
	}
	out := "["
	for i, e := range w {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("{ name = %q, ip = %q }", e.Name, e.IP)
	}
	return out + "]"
}

func normalizeClipboardBackend(v string) string {
	switch v {
	case "wayland", "xclip", "xsel":
		return v
	default:
		return "auto"
	}
}

func normalizeLogLevel(v string) string {
	switch v {
	case "", "debug", "info", "warn", "warning", "error":
		return v
	default:
		return "info"
	}
}

func normalizeInterfaces(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (c *Config) BroadcastInterval() time.Duration {
	if c.Broadcast <= 0 {
		return 5 * time.Second
	}
	return time.Duration(c.Broadcast) * time.Second
}

func (c *Config) WatchInterval() time.Duration {
	if c.Watch < 50 {
		c.Watch = 300
	}
	return time.Duration(c.Watch) * time.Millisecond
}
