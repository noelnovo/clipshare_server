package discover

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/grandcat/zeroconf"

	"clipshare/src/internal/config"
	"clipshare/src/internal/consts"
	"clipshare/src/internal/log"
)

// Advertise announces the daemon over mDNS on all interfaces. tls reports
// whether clients must use wss://.
func Advertise(ctx context.Context, name string, port int, tls bool) error {
	return advertise(ctx, name, port, tls, nil)
}

// advertise registers the mDNS service on ifaces, or on all interfaces when
// ifaces is nil.
func advertise(ctx context.Context, name string, port int, tls bool, ifaces []net.Interface) error {
	server, err := zeroconf.Register(name, consts.ServiceType, "local.", port,
		[]string{"name=" + name, "tls=" + strconv.FormatBool(tls)}, ifaces)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		server.Shutdown()
	}()
	log.Infof("mdns: advertising %s.%s on port %d (tls=%v)", name, consts.ServiceType, port, tls)
	return nil
}

// advertiseIfEnabled starts mDNS advertising when configured to do so.
// When discovery.mdns_subnets is set, only interfaces with an address in one
// of those ranges are announced, so virtual adapters (VirtualBox, WSL) are not
// advertised to the LAN.
func advertiseIfEnabled(ctx context.Context, cfg *config.Config) error {
	if !cfg.Discovery.MDNS {
		log.Infof("mdns advertising disabled")
		return nil
	}
	ifaces, err := mdnsInterfaces(cfg.Discovery.MDNSSubnets)
	if err != nil {
		return err
	}
	if len(cfg.Discovery.MDNSSubnets) > 0 && len(ifaces) == 0 {
		// Never fall back to all interfaces when the filter matches nothing.
		log.Warnf("mdns: no interface in %v; advertising disabled", cfg.Discovery.MDNSSubnets)
		return nil
	}
	return advertise(ctx, cfg.DeviceName, cfg.Server.Port, cfg.TLS.Enabled, ifaces)
}

// mdnsInterfaces returns the interfaces whose IPv4 address lies in one of the
// subnets. An empty subnet list returns nil, meaning all interfaces.
func mdnsInterfaces(subnets []string) ([]net.Interface, error) {
	if len(subnets) == 0 {
		return nil, nil
	}
	networks := make([]*net.IPNet, 0, len(subnets))
	for _, s := range subnets {
		_, network, err := net.ParseCIDR(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("discovery.mdns_subnets %q: %w", s, err)
		}
		networks = append(networks, network)
	}
	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var matched []net.Interface
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := addressIP(addr)
			if ip == nil {
				continue
			}
			if inAnyNetwork(ip, networks) {
				matched = append(matched, iface)
				break
			}
		}
	}
	return matched, nil
}

func inAnyNetwork(ip net.IP, networks []*net.IPNet) bool {
	for _, n := range networks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
