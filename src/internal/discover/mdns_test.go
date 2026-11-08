package discover

import (
	"net"
	"testing"
)

func TestMDNSInterfacesEmptyFilterMeansAll(t *testing.T) {
	ifaces, err := mdnsInterfaces(nil)
	if err != nil {
		t.Fatalf("mdnsInterfaces(nil) error: %v", err)
	}
	if ifaces != nil {
		t.Fatalf("empty filter should return nil (all interfaces), got %d", len(ifaces))
	}
}

func TestMDNSInterfacesRejectsInvalidSubnet(t *testing.T) {
	if _, err := mdnsInterfaces([]string{"not-a-cidr"}); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestInAnyNetwork(t *testing.T) {
	_, lan, err := net.ParseCIDR("192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	networks := []*net.IPNet{lan}
	if !inAnyNetwork(net.ParseIP("192.168.1.142"), networks) {
		t.Error("192.168.1.142 should match 192.168.1.0/24")
	}
	if inAnyNetwork(net.ParseIP("192.168.56.1"), networks) {
		t.Error("192.168.56.1 should not match 192.168.1.0/24")
	}
}
