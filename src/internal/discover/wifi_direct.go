package discover

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"

	"clipshare/src/internal/config"
	"clipshare/src/internal/consts"
	"clipshare/src/internal/log"
)

const wifiDirectPollInterval = 3 * time.Second

// wifiDirectAllowed reports whether Wi-Fi Direct may run. P2P is mTLS-only:
// enabling it while TLS is off must not advertise a plaintext endpoint.
func wifiDirectAllowed(cfg *config.Config) bool {
	return cfg.WiFiDirect.Enabled && cfg.TLS.Enabled
}

// startWiFiDirect advertises the daemon over DNS-SD on an already-created
// Wi-Fi Direct interface. It deliberately does not create a P2P group: that
// operation is controlled by Android, Windows, or NetworkManager.
func startWiFiDirect(ctx context.Context, cfg *config.Config) error {
	if !wifiDirectAllowed(cfg) {
		if cfg.WiFiDirect.Enabled {
			log.Warnf("wifi direct: disabled because [tls].enabled = false; Wi-Fi Direct requires mutual TLS")
		}
		return nil
	}

	go runWiFiDirectAdvertisement(ctx, cfg)
	return nil
}

func runWiFiDirectAdvertisement(ctx context.Context, cfg *config.Config) {
	var advertised *zeroconf.Server
	lastKey := ""
	ticker := time.NewTicker(wifiDirectPollInterval)
	defer ticker.Stop()

	refresh := func() {
		interfaces, ips := findWiFiDirectInterfaces(cfg.WiFiDirect.Interfaces)
		key := interfaceKey(interfaces, ips)
		if key == lastKey {
			return
		}
		lastKey = key

		if advertised != nil {
			advertised.Shutdown()
			advertised = nil
		}
		if len(interfaces) == 0 || len(ips) == 0 {
			log.Debugf("wifi direct: no P2P interface found")
			return
		}

		host := sanitizeHost(cfg.DeviceName) + ".local."
		server, err := zeroconf.RegisterProxy(
			cfg.DeviceName,
			consts.ServiceType,
			"local.",
			cfg.Server.Port,
			host,
			ips,
			[]string{
				"name=" + cfg.DeviceName,
				"tls=" + fmt.Sprint(cfg.TLS.Enabled),
				"platform=desktop",
				"transport=wifi_direct",
			},
			interfaces,
		)
		if err != nil {
			log.Warnf("wifi direct: advertise failed: %v", err)
			return
		}
		advertised = server
		log.Infof("wifi direct: advertising %s on %s", cfg.DeviceName, strings.Join(ips, ","))
	}

	refresh()
	for {
		select {
		case <-ctx.Done():
			if advertised != nil {
				advertised.Shutdown()
			}
			return
		case <-ticker.C:
			refresh()
		}
	}
}

func findWiFiDirectInterfaces(configured []string) ([]net.Interface, []string) {
	all, err := net.Interfaces()
	if err != nil {
		log.Debugf("wifi direct: list interfaces: %v", err)
		return nil, nil
	}

	interfaces := make([]net.Interface, 0)
	ips := make([]string, 0)
	seenIPs := make(map[string]struct{})
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if len(configured) > 0 && !matchesWiFiDirectInterface(iface.Name, configured) {
			continue
		}
		if len(configured) == 0 && !matchesWiFiDirectInterface(iface.Name, nil) && !interfaceHasP2PAddress(iface) {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		matched := false
		for _, address := range addresses {
			ip := addressIP(address)
			if ip == nil || (len(configured) == 0 && !matchesWiFiDirectAddress(ip)) {
				continue
			}
			matched = true
			value := ip.String()
			if _, exists := seenIPs[value]; !exists {
				seenIPs[value] = struct{}{}
				ips = append(ips, value)
			}
		}
		if matched {
			interfaces = append(interfaces, iface)
		}
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Index < interfaces[j].Index })
	sort.Strings(ips)
	return interfaces, ips
}

func interfaceHasP2PAddress(iface net.Interface) bool {
	addresses, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		if ip := addressIP(address); ip != nil && matchesWiFiDirectAddress(ip) {
			return true
		}
	}
	return false
}

func matchesWiFiDirectInterface(name string, configured []string) bool {
	if len(configured) > 0 {
		for _, value := range configured {
			if strings.EqualFold(strings.TrimSpace(value), name) {
				return true
			}
		}
		return false
	}
	lower := strings.ToLower(name)
	return strings.Contains(lower, "p2p") ||
		strings.Contains(lower, "wi-fi direct") ||
		strings.Contains(lower, "wifi direct") ||
		strings.Contains(lower, "wifidirect")
}

func matchesWiFiDirectAddress(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}
	// These are the common Android and Windows Wi-Fi Direct group ranges.
	return ip[0] == 192 && ip[1] == 168 &&
		((ip[2] == 49) || (ip[2] == 137))
}

func addressIP(address net.Addr) net.IP {
	switch value := address.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		valueText := strings.Split(address.String(), "/")[0]
		return net.ParseIP(valueText)
	}
}

func interfaceKey(interfaces []net.Interface, ips []string) string {
	parts := make([]string, 0, len(interfaces)+len(ips))
	for _, iface := range interfaces {
		parts = append(parts, fmt.Sprintf("%d:%s", iface.Index, iface.Name))
	}
	parts = append(parts, ips...)
	return strings.Join(parts, "|")
}

func sanitizeHost(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	host := strings.Trim(b.String(), "-")
	if host == "" {
		return "clipshare"
	}
	return host
}
