//go:build !darwin || !cgo

package main

// Pure-Go fallback for the Bonjour registration (bonjour_darwin.go).
// Used on Linux/Windows, and on macOS builds with CGO_ENABLED=0 (the
// cross-compiled release binaries). startMDNS falls back to the multicast
// responder, which advertises <name>.local to other LAN devices; the host
// itself uses localhost (which the TLS cert covers).

import (
	"log"
	"net"
)

// tryBonjourRegister is a no-op on platforms without the native Bonjour
// path. It always returns false so startMDNS uses the multicast responder.
func tryBonjourRegister(name string, ip net.IP, port int, secure bool) bool {
	log.Printf("mDNS: Bonjour registration unavailable in this build; using multicast responder")
	return false
}
