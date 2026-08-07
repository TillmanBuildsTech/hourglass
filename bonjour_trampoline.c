//go:build darwin && cgo

// C trampoline for the Bonjour record callback. Lives in its own .c file
// (not the cgo preamble) so it is compiled exactly once as a real external
// symbol: a preamble that uses //export must contain declarations only
// (definitions there get copied into two generated C files and collide at
// link time), and a static preamble function would have internal linkage
// invisible to the Go runtime. This file is the documented cgo pattern for
// calling back into Go from a C API.

#include <dns_sd.h>

// Provided by the //export goRecordConflict directive in bonjour_darwin.go.
extern void goRecordConflict(DNSServiceRef, DNSRecordRef, DNSServiceFlags, DNSServiceErrorType, void*);

void recordConflictTrampoline(DNSServiceRef sdRef, DNSRecordRef RecordRef,
                              DNSServiceFlags flags, DNSServiceErrorType errorCode,
                              void *context) {
    goRecordConflict(sdRef, RecordRef, flags, errorCode, context);
}
