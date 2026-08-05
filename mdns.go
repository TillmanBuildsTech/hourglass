package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/grandcat/zeroconf"
)

// startMDNS advertises the Hourglass web UI on the local network via mDNS
// (Bonjour), so it can be reached at http://<name>.local from any device on
// the LAN. RFC 6762 defines .local as the mDNS TLD; ".lan" is a router/DNS
// convention, not something mDNS answers — so the advertised name is always
// <name>.local.
//
// Advertisement only makes sense when the server is actually reachable on
// the LAN, so it is skipped when HOURGLASS_BIND points at a loopback
// address. Disable with HOURGLASS_MDNS=0; override the name with
// HOURGLASS_MDNS_NAME (default "hourglass").
func startMDNS(addr string) {
	if os.Getenv("HOURGLASS_MDNS") == "0" {
		log.Printf("mDNS advertisement disabled (HOURGLASS_MDNS=0)")
		return
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		log.Printf("mDNS advertisement skipped: cannot parse bind address %q", addr)
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Printf("mDNS advertisement skipped: invalid port %q", portStr)
		return
	}
	ip := lanIP(host)
	if ip == nil {
		log.Printf("mDNS advertisement skipped: bind host %q is loopback (set HOURGLASS_BIND=0.0.0.0:8080 to expose on the LAN)", host)
		return
	}
	name := os.Getenv("HOURGLASS_MDNS_NAME")
	if name == "" {
		name = "hourglass"
	}
	server, err := zeroconf.RegisterProxy(
		"Hourglass", "_http._tcp.", "local.", port,
		name, []string{ip.String()}, []string{fmt.Sprintf("version=%s", version())}, nil,
	)
	if err != nil {
		log.Printf("mDNS advertisement failed: %v", err)
		return
	}
	log.Printf("mDNS: http://%s.local:%d — %s", name, port, ip)
	// server must outlive this call; the HTTP server below blocks for the
	// process lifetime, and this reference keeps the responder alive.
	_ = server
}

// lanIP returns the IP the mDNS responder should advertise for this host:
// the bind host itself when it is a specific non-loopback address, otherwise
// the primary outbound interface IP (preferring a private LAN address).
func lanIP(bindHost string) net.IP {
	if bindHost != "" && bindHost != "0.0.0.0" && bindHost != "::" {
		ip := net.ParseIP(bindHost)
		if ip != nil && !ip.IsLoopback() {
			return ip
		}
		return nil
	}
	// Primary outbound IP: the address the kernel would use to reach the
	// internet. Works behind NAT and ignores docker bridges etc.
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		defer conn.Close()
		if local, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			if ip4 := local.IP.To4(); ip4 != nil {
				return ip4
			}
		}
	}
	// Fallback: first private non-loopback IPv4 interface.
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			ip4 := ipnet.IP.To4()
			if ip4 != nil && !ip4.IsLoopback() && ip4.IsPrivate() {
				return ip4
			}
		}
	}
	return nil
}
