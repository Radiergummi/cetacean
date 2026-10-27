package netguard

import (
	"errors"
	"net"
	"testing"
)

// An IPv6 form that carries an IPv4 address reaches that address, so it is
// screened by the IPv4 rules.
func TestCheckScreensTheIPv4AnIPv6AddressEmbeds(t *testing.T) {
	for _, addr := range []string{
		"64:ff9b::a9fe:a9fe",   // NAT64 to 169.254.169.254
		"64:ff9b::a00:1",       // NAT64 to 10.0.0.1
		"64:ff9b:1::5db8:d822", // local-use NAT64, refused outright
		"2002:a9fe:a9fe::1",    // 6to4 from 169.254.169.254
		"2002:7f00:1::1",       // 6to4 from 127.0.0.1
		"::a9fe:a9fe",          // IPv4-compatible 169.254.169.254
		"::7f00:1",             // IPv4-compatible 127.0.0.1
	} {
		if err := (Policy{}).Check(net.ParseIP(addr)); !errors.Is(err, ErrBlocked) {
			t.Errorf("Check(%s) = %v, want ErrBlocked", addr, err)
		}
	}

	for _, addr := range []string{"64:ff9b::5db8:d822", "2002:5db8:d822::1", "2606:4700::1111"} {
		if err := (Policy{}).Check(net.ParseIP(addr)); err != nil {
			t.Errorf("Check(%s) = %v, want a public address allowed", addr, err)
		}
	}

	if err := (Policy{AllowLoopback: true}).Check(net.ParseIP("::1")); err != nil {
		t.Errorf("Check(::1) with loopback allowed = %v", err)
	}

	if err := (Policy{AllowPrivate: true}).Check(net.ParseIP("64:ff9b::a00:1")); err != nil {
		t.Errorf("Check(NAT64 10.0.0.1) with private allowed = %v", err)
	}
}
