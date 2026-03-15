package main

import (
	"net"
	"net/netip"
	"testing"
)

func Test_getPublicIPv6(t *testing.T) {
	const globalStr = "2001:db8::68"

	getter := func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("192.168.1.1"), Mask: net.CIDRMask(24, net.IPv4len*8)},
			&net.IPNet{IP: net.ParseIP("fd02::1"), Mask: net.CIDRMask(96, net.IPv6len*8)},
			&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, net.IPv6len*8)},
			&net.IPNet{IP: net.ParseIP(globalStr), Mask: net.CIDRMask(64, net.IPv6len*8)},
		}, nil
	}

	interfaceAddrsGetter = getter
	defer func() {
		interfaceAddrsGetter = net.InterfaceAddrs
	}()

	ip, err := getPublicIPv6()
	if err != nil {
		t.Fatalf("getPublicIPv6() error = %v", err)
	}

	if public := netip.MustParseAddr(globalStr); ip != public {
		t.Errorf("getPublicIPv6() = %v, want %v", ip, public)
	}
}
