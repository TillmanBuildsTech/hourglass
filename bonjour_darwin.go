//go:build darwin && cgo

package main

// Native Bonjour registration via mDNSResponder (the Bonjour API), used on
// macOS when the binary is built with cgo (e.g. via the Homebrew formula,
// which runs `go build` natively on the Mac).
//
// Why this exists: the pure-Go multicast responder in mdns.go makes
// <name>.local resolvable from OTHER devices, but macOS's own mDNSResponder
// often fails to see its own host's multicast announcements — so the Mac
// itself cannot resolve its own hourglass.local (browsers fall back to
// localhost, which works but without the .local identity the TLS cert is
// issued for). Registering the A record + service with mDNSResponder via
// DNSServiceRegisterRecord/DNSServiceRegister makes the OS own the name:
// the local host and every LAN device resolve it, and the OS handles
// conflict detection instead of our probe.
//
// The cgo API is tiny and stable (dns_sd.h, part of libSystem since macOS
// 10.4). The stub build (CGO_ENABLED=0, Linux, Windows) returns false so
// startMDNS falls back to the multicast responder.

/*
#include <dns_sd.h>
#include <stdlib.h>

// Trampoline so the C callback can call back into Go (cgo rule: a Go
// function pointer must not be passed directly to C).
static void goRecordConflict(DNSServiceRef, DNSRecordRef, DNSServiceFlags, DNSServiceErrorType, void*);

static void recordConflictTrampoline(DNSServiceRef sdRef, DNSRecordRef RecordRef,
                                     DNSServiceFlags flags, DNSServiceErrorType errorCode,
                                     void *context) {
    goRecordConflict(sdRef, RecordRef, flags, errorCode, context);
}
*/
import "C"

import (
	"log"
	"net"
	"unsafe"
)

// bonjourRefs holds the DNSServiceRefs for the process lifetime. The OS
// keeps serving registered records as long as their socket is open;
// deallocating the ref would unregister the records.
var bonjourRefs []C.DNSServiceRef

// bonjourName remembers the advertised name for the async conflict callback
// (cgo cannot safely pass a Go string through the C context pointer, and
// only one name is registered per process).
var bonjourName string

// tryBonjourRegister registers <name>.local as an A record plus the
// Hourglass service (_https/_http._tcp) with macOS mDNSResponder, so the
// name resolves on the host itself and on the LAN. Returns true when
// registration was accepted; false (logged) when it can't be done — the
// caller then falls back to the multicast responder.
func tryBonjourRegister(name string, ip net.IP, port int, secure bool) bool {
	bonjourName = name
	ip4 := ip.To4()
	if ip4 == nil {
		log.Printf("mDNS: Bonjour registration skipped: %s is not an IPv4 address", ip)
		return false
	}

	// A connected DNSServiceRef is required for DNSServiceRegisterRecord.
	var connRef C.DNSServiceRef
	if serr := C.DNSServiceCreateConnection(&connRef); serr != C.kDNSServiceErr_NoError {
		log.Printf("mDNS: Bonjour could not create a connection (%d); using multicast responder", int32(serr))
		return false
	}

	// Full record name: "<name>.local."
	cname := C.CString(name + ".local.")
	defer C.free(unsafe.Pointer(cname))

	var rdata [4]byte
	copy(rdata[:], ip4)

	var recRef C.DNSRecordRef
	serr := C.DNSServiceRegisterRecord(
		connRef,                  // connected DNSServiceRef (by value)
		&recRef,                  // DNSRecordRef output
		C.kDNSServiceFlagsUnique, // we own this name
		0,                        // interfaceIndex: all interfaces
		cname,                    // fullname
		C.kDNSServiceType_A,
		C.kDNSServiceClass_IN,
		4, // rdlen
		unsafe.Pointer(&rdata[0]),
		120,                        // ttl seconds
		C.recordConflictTrampoline, // callBack (logs async conflicts)
		nil,                        // context
	)
	if serr != C.kDNSServiceErr_NoError {
		C.DNSServiceRefDeallocate(connRef)
		log.Printf("mDNS: Bonjour could not register %s.local (%d); using multicast responder", name, int32(serr))
		return false
	}

	// Service record so Bonjour browsers and clients see "Hourglass" at
	// _https._tcp / _http._tcp on the LAN.
	regType := "_http._tcp"
	if secure {
		regType = "_https._tcp"
	}
	creg := C.CString(regType)
	defer C.free(unsafe.Pointer(creg))
	cinst := C.CString("Hourglass")
	defer C.free(unsafe.Pointer(cinst))

	var sRef C.DNSServiceRef
	serr = C.DNSServiceRegister(
		&sRef,
		0,     // flags: defaults (auto-rename on conflict)
		0,     // interfaceIndex: all interfaces
		cinst, // name (instance)
		creg,  // regtype
		nil,   // domain: default (.local)
		nil,   // host: default hostname
		C.uint16_t((uint16(port)>>8)|(uint16(port)<<8)), // port — network byte order (htons is a macro, unavailable to cgo)
		0,   // txtLen
		nil, // txtRecord
		nil, // callBack (NULL allowed for DNSServiceRegister)
		nil, // context
	)
	if serr != C.kDNSServiceErr_NoError {
		log.Printf("mDNS: Bonjour service registration failed (%d); A record stays registered", int32(serr))
		// A-record registration already succeeded — keep it; the name will
		// still resolve, only service discovery is missing.
		bonjourRefs = append(bonjourRefs, connRef)
		go bonjourProcess(connRef, name)
		return true
	}

	bonjourRefs = append(bonjourRefs, connRef, sRef)
	go bonjourProcess(connRef, name)
	go bonjourProcess(sRef, name)
	log.Printf("mDNS: registered %s.local — %s via Bonjour (macOS)", name, ip)
	return true
}

// goRecordConflict is the //export'd callback invoked by mDNSResponder for
// async registration events. A non-zero errorCode means the record was not
// (or no longer) served — most commonly kDNSServiceErr_NameConflict.
//
//export goRecordConflict
func goRecordConflict(sdRef C.DNSServiceRef, recordRef C.DNSRecordRef, flags C.DNSServiceFlags, errorCode C.DNSServiceErrorType, context unsafe.Pointer) {
	if errorCode != C.kDNSServiceErr_NoError {
		log.Printf("mDNS: Bonjour record error (code %d) — %s.local may not resolve; use localhost", int32(errorCode), bonjourName)
	}
}

// bonjourProcess keeps a DNSServiceRef serviced. With no callbacks installed
// this mostly parks on the socket; it returns when the ref is invalidated.
func bonjourProcess(ref C.DNSServiceRef, name string) {
	for {
		if res := C.DNSServiceProcessResult(ref); res != C.kDNSServiceErr_NoError {
			log.Printf("mDNS: Bonjour registration for %s.local ended (%d)", name, int32(res))
			return
		}
	}
}
