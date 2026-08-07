package main

// HTTP→HTTPS redirect on the same port.
//
// Hourglass serves HTTPS from the same port it binds (8080 by default), so
// a plain "http://localhost:8080" (or http://hourglass.local:8080) request
// hits the TLS listener and is dropped — browsers show "connection reset"
// or a TLS error instead of the app. This file wraps the TLS listener so a
// non-TLS request on the same port gets a 308 redirect to https:// instead
// of a hard failure.
//
// It works by sniffing the first byte of every connection: TLS ClientHello
// records start with 0x16, plain HTTP does not. TLS connections pass through
// untouched; everything else is served a redirect by a plain-HTTP handler.

import (
	"bufio"
	"crypto/tls"
	"net"
	"net/http"
)

// sniffListener peeks the first byte of each accepted connection to decide
// whether it carries TLS (0x16 = handshake) or plain HTTP.
type sniffListener struct {
	net.Listener
	tlsConfig *tls.Config
}

// bufferedConn replays the bytes already read by the sniffer before handing
// the connection to the server, so nothing is lost.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// Accept returns either a tls.Conn (when the client speaks TLS) or a plain
// bufferedConn (when it does not).
func (l *sniffListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	if err != nil {
		c.Close()
		return nil, err
	}
	bc := &bufferedConn{Conn: c, r: br}
	if first[0] == 0x16 { // TLS ClientHello
		return tls.Server(bc, l.tlsConfig), nil
	}
	return bc, nil
}

// serveTLSWithRedirect serves HTTPS on addr and 308-redirects plain-HTTP
// requests to https:// on the same host:port. The redirect handler runs
// before the app's auth middleware, so unauthenticated http:// clients are
// bounced to the secure URL rather than getting a 401.
func serveTLSWithRedirect(addr, certFile, keyFile string, app http.Handler) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return serveTLSWithRedirectListener(ln, certFile, keyFile, app)
}

// serveTLSWithRedirectListener is serveTLSWithRedirect over an existing
// listener (kept separate so tests can bind 127.0.0.1:0 and learn the port).
func serveTLSWithRedirectListener(ln net.Listener, certFile, keyFile string, app http.Handler) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			app.ServeHTTP(w, r)
			return
		}
		u := *r.URL
		u.Scheme = "https"
		u.Host = r.Host
		http.Redirect(w, r, u.String(), http.StatusPermanentRedirect)
	})
	srv := &http.Server{
		Handler:   redirect,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	return srv.Serve(&sniffListener{Listener: ln, tlsConfig: srv.TLSConfig})
}
