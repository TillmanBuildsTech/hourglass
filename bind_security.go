package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

// enforceBindSecurity fails closed when Hourglass would be reachable from
// the network without credentials. The web UI can run arbitrary shell
// commands, so an unauthenticated LAN-accessible instance is a remote code
// execution hole.
//
// Rules:
//   - loopback binds (127.0.0.1, localhost, ::1) — always allowed, no auth
//     needed (only the local user can reach it)
//   - non-loopback binds (0.0.0.0, LAN IPs) — require HOURGLASS_AUTH_USER +
//     HOURGLASS_AUTH_PASS, unless HOURGLASS_ALLOW_INSECURE=1 is set
//     explicitly (documented footgun for trusted networks)
func enforceBindSecurity(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid HOURGLASS_BIND %q: %w", addr, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil // loopback hostname — local user only
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return requireAuthForExposure(addr)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Hostname we can't classify (e.g. "localhost", a LAN hostname, or a
		// bogus value). Treat unknown hostnames as non-loopback so we fail
		// closed rather than serving unauthenticated on a network interface.
		return requireAuthForExposure(addr)
	}
	if !ip.IsLoopback() {
		// Specific non-loopback IP (e.g. HOURGLASS_BIND=192.168.1.90:8080)
		return requireAuthForExposure(addr)
	}
	return nil
}

func requireAuthForExposure(addr string) error {
	hasCreds := os.Getenv("HOURGLASS_AUTH_USER") != "" && os.Getenv("HOURGLASS_AUTH_PASS") != ""
	if hasCreds {
		return nil
	}
	if os.Getenv("HOURGLASS_ALLOW_INSECURE") == "1" {
		log.Printf("WARNING: serving WITHOUT authentication on %s (HOURGLASS_ALLOW_INSECURE=1) — anyone on the network can run commands as this user", addr)
		return nil
	}
	return fmt.Errorf(
		"HOURGLASS_BIND=%s exposes the web UI on all interfaces, but no credentials are configured. "+
			"Set HOURGLASS_AUTH_USER and HOURGLASS_AUTH_PASS, bind to 127.0.0.1:8080, "+
			"or explicitly opt in to an open server with HOURGLASS_ALLOW_INSECURE=1", addr)
}
