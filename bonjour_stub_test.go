//go:build !darwin || !cgo

package main

import (
	"net"
	"testing"
)

// TestBonjourStubFallsBack ensures the non-cgo stub reports "not
// registered" so startMDNS uses the multicast responder. Build-tagged to
// match bonjour_stub.go: on darwin+cgo builds tryBonjourRegister is the real
// Bonjour implementation (which legitimately returns true on a Mac), so this
// test is excluded there.
func TestBonjourStubFallsBack(t *testing.T) {
	if tryBonjourRegister("hourglass", net.ParseIP("192.168.1.241"), 8080, true) {
		t.Fatal("stub tryBonjourRegister must return false (fall back to multicast)")
	}
}
