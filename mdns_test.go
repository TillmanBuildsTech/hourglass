package main

import (
	"net"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestMDNSServiceType(t *testing.T) {
	plain := &mdnsResponder{instance: "Hourglass", name: "hourglass", port: 8080, secure: false}
	if got := plain.serviceType(); got != "_http._tcp.local." {
		t.Errorf("serviceType() = %q, want _http._tcp.local.", got)
	}
	if got := plain.service(); got != "Hourglass._http._tcp.local." {
		t.Errorf("service() = %q, want Hourglass._http._tcp.local.", got)
	}

	secure := &mdnsResponder{instance: "Hourglass", name: "hourglass", port: 8443, secure: true}
	if got := secure.serviceType(); got != "_https._tcp.local." {
		t.Errorf("serviceType() = %q, want _https._tcp.local.", got)
	}
	if got := secure.service(); got != "Hourglass._https._tcp.local." {
		t.Errorf("service() = %q, want Hourglass._https._tcp.local.", got)
	}
}

// TestMDNSBuildResponseSecure checks the actual wire payload for a TLS
// responder: the PTR/SRV records must use _https._tcp.local. so Bonjour
// clients (macOS Finder, iOS, avahi) present https://hourglass.local.
func TestMDNSBuildResponseSecure(t *testing.T) {
	r := &mdnsResponder{
		instance: "Hourglass",
		name:     "hourglass",
		port:     8443,
		ip:       net.ParseIP("192.168.1.90"),
		secure:   true,
		txt:      "version=test",
	}
	raw := r.buildResponse()
	if len(raw) == 0 {
		t.Fatal("buildResponse returned empty payload")
	}
	var msg dnsmessage.Message
	if err := msg.Unpack(raw); err != nil {
		t.Fatalf("response does not unpack: %v", err)
	}
	if !msg.Header.Response {
		t.Error("response header missing the Response flag")
	}
	var sawPTR, sawSRV, sawA, sawTXT bool
	for _, a := range msg.Answers {
		switch a.Header.Type {
		case dnsmessage.TypePTR:
			if a.Header.Name.String() != "_https._tcp.local." {
				t.Errorf("PTR owner = %q, want _https._tcp.local.", a.Header.Name)
			}
			sawPTR = true
		case dnsmessage.TypeSRV:
			if a.Header.Name.String() != "Hourglass._https._tcp.local." {
				t.Errorf("SRV owner = %q, want Hourglass._https._tcp.local.", a.Header.Name)
			}
			if srv, ok := a.Body.(*dnsmessage.SRVResource); ok {
				if srv.Port != 8443 {
					t.Errorf("SRV port = %d, want 8443", srv.Port)
				}
				if !strings.HasPrefix(srv.Target.String(), "hourglass.local.") {
					t.Errorf("SRV target = %q, want hourglass.local.", srv.Target)
				}
			}
			sawSRV = true
		case dnsmessage.TypeA:
			sawA = true
		case dnsmessage.TypeTXT:
			sawTXT = true
		}
	}
	if !sawPTR || !sawSRV || !sawA || !sawTXT {
		t.Errorf("missing records: PTR=%v SRV=%v A=%v TXT=%v", sawPTR, sawSRV, sawA, sawTXT)
	}
}
