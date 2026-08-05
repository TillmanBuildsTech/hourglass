package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

// startMDNS advertises the Hourglass web UI on the local network via mDNS
// (Bonjour), so it can be reached at http://<name>.local from any device on
// the LAN. RFC 6762 defines .local as the mDNS TLD; ".lan" is a router/DNS
// convention, not something mDNS answers — so the advertised name is always
// <name>.local.
//
// This is a minimal self-contained responder (UDP multicast on 224.0.0.251:
// 5353) using only x/net/dns/dnsmessage for packet encoding — deliberately
// no external mDNS library, so the binary stays dependency-light and links
// cleanly on every platform (including macOS, where newer x/net releases
// fail at link time).
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

	r := &mdnsResponder{
		instance: "Hourglass",
		name:     name,
		port:     port,
		ip:       ip,
		txt:      fmt.Sprintf("version=%s", version()),
	}
	if err := r.run(); err != nil {
		log.Printf("mDNS advertisement failed: %v", err)
		return
	}
	log.Printf("mDNS: http://%s.local:%d — %s", name, port, ip)
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

const mdnsGroup = "224.0.0.251"

// Cache-flush bit for unique records (RFC 6762 §10.2).
const cacheFlush = 0x8000

type mdnsResponder struct {
	instance string // "Hourglass"
	name     string // "hourglass"
	port     int
	ip       net.IP
	txt      string
}

func (r *mdnsResponder) host() string        { return r.name + ".local." }
func (r *mdnsResponder) service() string     { return r.instance + "._http._tcp.local." }
func (r *mdnsResponder) serviceType() string { return "_http._tcp.local." }

func (r *mdnsResponder) run() error {
	iface := interfaceWithIP(r.ip)

	conn, err := net.ListenMulticastUDP("udp4", iface, &net.UDPAddr{IP: net.ParseIP(mdnsGroup), Port: 5353})
	if err != nil {
		return fmt.Errorf("listen multicast: %w", err)
	}
	go func() {
		defer conn.Close()
		r.announce(conn)
		buf := make([]byte, 1500)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if src.IP.IsLoopback() {
				continue
			}
			r.handleQuery(conn, buf[:n])
		}
	}()
	return nil
}

func interfaceWithIP(ip net.IP) *net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for i := range ifaces {
		addrs, err := ifaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.Equal(ip) {
				return &ifaces[i]
			}
		}
	}
	return nil
}

func (r *mdnsResponder) announce(conn *net.UDPConn) {
	// Unsolicited response burst so clients learn about us immediately
	// without having to query (RFC 6762 §8.3 announcing).
	msg := r.buildResponse()
	addr := &net.UDPAddr{IP: net.ParseIP(mdnsGroup), Port: 5353}
	for i := 0; i < 3; i++ {
		if _, err := conn.WriteToUDP(msg, addr); err != nil {
			return
		}
	}
}

func (r *mdnsResponder) handleQuery(conn *net.UDPConn, buf []byte) {
	var msg dnsmessage.Message
	if err := msg.Unpack(buf); err != nil {
		return
	}
	if len(msg.Questions) == 0 {
		return
	}
	want := false
	for _, q := range msg.Questions {
		name := strings.ToLower(q.Name.String())
		switch q.Type {
		case dnsmessage.TypeA, dnsmessage.TypeALL:
			if name == r.host() {
				want = true
			}
		case dnsmessage.TypePTR:
			if name == r.serviceType() {
				want = true
			}
		case dnsmessage.TypeSRV:
			if name == r.service() {
				want = true
			}
		case dnsmessage.TypeTXT:
			if name == r.service() {
				want = true
			}
		}
	}
	if !want {
		return
	}
	addr := &net.UDPAddr{IP: net.ParseIP(mdnsGroup), Port: 5353}
	if _, err := conn.WriteToUDP(r.buildResponse(), addr); err != nil {
		log.Printf("mDNS: write failed: %v", err)
	}
}

func (r *mdnsResponder) buildResponse() []byte {
	ip4 := r.ip.To4()
	if ip4 == nil {
		return nil
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true})
	b.EnableCompression()
	if err := b.StartAnswers(); err != nil {
		return nil
	}

	// PTR: _http._tcp.local. -> Hourglass._http._tcp.local.
	if err := b.PTRResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName(r.serviceType()),
		Type:  dnsmessage.TypePTR,
		Class: dnsmessage.ClassINET | cacheFlush,
		TTL:   120,
	}, dnsmessage.PTRResource{PTR: dnsmessage.MustNewName(r.service())}); err != nil {
		return nil
	}
	// SRV: Hourglass._http._tcp.local. -> hourglass.local:port
	if err := b.SRVResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName(r.service()),
		Type:  dnsmessage.TypeSRV,
		Class: dnsmessage.ClassINET | cacheFlush,
		TTL:   120,
	}, dnsmessage.SRVResource{
		Target: dnsmessage.MustNewName(r.host()),
		Port:   uint16(r.port),
	}); err != nil {
		return nil
	}
	// TXT: version=...
	if err := b.TXTResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName(r.service()),
		Type:  dnsmessage.TypeTXT,
		Class: dnsmessage.ClassINET | cacheFlush,
		TTL:   120,
	}, dnsmessage.TXTResource{TXT: []string{r.txt}}); err != nil {
		return nil
	}
	// A: hourglass.local -> ip
	if err := b.AResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName(r.host()),
		Type:  dnsmessage.TypeA,
		Class: dnsmessage.ClassINET | cacheFlush,
		TTL:   120,
	}, dnsmessage.AResource{A: [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]}}); err != nil {
		return nil
	}

	out, err := b.Finish()
	if err != nil {
		return nil
	}
	return out
}
