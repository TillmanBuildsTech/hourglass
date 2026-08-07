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

// TestMDNSProbeQuery verifies the conflict-probe query asks for the A record
// of our hostname (RFC 6762 §8.1 probing).
func TestMDNSProbeQuery(t *testing.T) {
	r := &mdnsResponder{instance: "Hourglass", name: "hourglass", port: 8080, ip: net.ParseIP("192.168.1.241")}
	raw := r.buildProbeQuery()
	if len(raw) == 0 {
		t.Fatal("buildProbeQuery returned empty payload")
	}
	var msg dnsmessage.Message
	if err := msg.Unpack(raw); err != nil {
		t.Fatalf("probe query does not unpack: %v", err)
	}
	if len(msg.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(msg.Questions))
	}
	q := msg.Questions[0]
	if q.Name.String() != "hourglass.local." {
		t.Errorf("question name = %q, want hourglass.local.", q.Name)
	}
	if q.Type != dnsmessage.TypeA {
		t.Errorf("question type = %v, want A", q.Type)
	}
}

// TestMDNSResponseClaimsName covers the conflict detector: an A answer for
// our hostname with a different IP is a conflict; our own IP or a different
// hostname is not.
func TestMDNSResponseClaimsName(t *testing.T) {
	ourIP := net.ParseIP("192.168.1.241")

	build := func(name string, ip net.IP) dnsmessage.Message {
		b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true})
		b.StartAnswers()
		b.AResource(dnsmessage.ResourceHeader{
			Name:  dnsmessage.MustNewName(name),
			Type:  dnsmessage.TypeA,
			Class: dnsmessage.ClassINET,
			TTL:   120,
		}, dnsmessage.AResource{A: [4]byte{ip[12], ip[13], ip[14], ip[15]}})
		raw, err := b.Finish()
		if err != nil {
			t.Fatalf("build response: %v", err)
		}
		var msg dnsmessage.Message
		if err := msg.Unpack(raw); err != nil {
			t.Fatalf("unpack response: %v", err)
		}
		return msg
	}

	// Another host answers our name with a different IP → conflict.
	if !mDNSResponseClaimsName(build("hourglass.local.", net.ParseIP("192.168.1.90")), "hourglass.local.", ourIP) {
		t.Error("different IP for our hostname should be a conflict")
	}
	// Our own IP answering our name → not a conflict (our own announcement).
	if mDNSResponseClaimsName(build("hourglass.local.", ourIP), "hourglass.local.", ourIP) {
		t.Error("our own IP should not be a conflict")
	}
	// A different hostname with a different IP → not our conflict.
	if mDNSResponseClaimsName(build("other.local.", net.ParseIP("192.168.1.90")), "hourglass.local.", ourIP) {
		t.Error("different hostname should not be a conflict")
	}
}

// TestMDNSProbeAndRenameRenamesOnConflict drives the real rename loop with an
// injected probe: a responder whose probe always reports a conflict ends up
// as <name>-N.local; a free name keeps the original.
func TestMDNSProbeAndRenameRenamesOnConflict(t *testing.T) {
	r := &mdnsResponder{instance: "Hourglass", name: "hourglass", ip: net.ParseIP("192.168.1.241")}
	// probe always reports "conflict" (name taken).
	conflict := func(*net.UDPConn) bool { return false }
	r.probeAndRenameWith(nil, conflict)
	if r.name != "hourglass-20" {
		t.Errorf("after max conflicts name = %q, want hourglass-20", r.name)
	}

	// Free name: probe reports no conflict, name stays the base.
	r2 := &mdnsResponder{instance: "Hourglass", name: "hourglass", ip: net.ParseIP("192.168.1.241")}
	free := func(*net.UDPConn) bool { return true }
	r2.probeAndRenameWith(nil, free)
	if r2.name != "hourglass" {
		t.Errorf("free name after probe = %q, want hourglass", r2.name)
	}

	// One conflict then free: renames once to -2.
	r3 := &mdnsResponder{instance: "Hourglass", name: "hourglass", ip: net.ParseIP("192.168.1.241")}
	calls := 0
	conflictOnce := func(*net.UDPConn) bool { calls++; return calls > 1 }
	r3.probeAndRenameWith(nil, conflictOnce)
	if r3.name != "hourglass-2" {
		t.Errorf("after one conflict name = %q, want hourglass-2", r3.name)
	}
}
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
