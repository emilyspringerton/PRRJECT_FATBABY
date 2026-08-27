// Package seqlock is PRRJECT_FATBABY's first real PARENA-authored mod:
// a cross-process advisory file lock protecting eventstore.FileStore's
// sequence-assignment critical section, wired through a PARENA-compiled
// C function rather than a direct Go patch. Founder, real-time
// (2026-08-25), after a live, confirmed, currently-occurring bug (a
// PRNewswire press release rendering with an unrelated NVIDIA SEC 8-K
// link, root-caused to a real cross-process sequence collision --
// sequence 112200 in one real journal file had 5 different records from
// 2 different OS processes): "fix it with parena mod api first."
//
// v0 scope, deliberately minimal: two functions (lock-acquire,
// lock-release, in stdlib/eventstore/seqlock.prn), no dispatch table, no
// plugin discovery, no config -- the narrowest real thing that makes
// "the fix is a PARENA mod" true. See EMILY/BACKLOG.md for the full
// root-cause writeup and the PITVIPER/EmilyOS mod-surface precedent
// (S189-29/34/40, S192-01, S193-02) this borrows its FFI pattern from.
//
// seqlock_mod.c in this package directory is PARENA-generated (`parena
// build stdlib/eventstore/seqlock.prn -o eventstore/seqlock/seqlock_mod.c`)
// -- do not hand-edit it; regenerate from the .prn source instead.
// seqlock_host.c is the real, hand-written host implementation of the
// two syscall-wrapper functions the generated C calls into.
package seqlock

/*
#cgo CFLAGS: -include ${SRCDIR}/seqlock_host.h
#include <stdlib.h>
extern int lock_acquire(char *path);
extern int lock_release(int fd);
*/
import "C"
import "unsafe"

// Acquire opens (creating if needed) the lock file at path and blocks
// until an exclusive advisory lock is held, entering the PARENA-compiled
// mod's lock_acquire (which calls the real eventstore_seqlock_acquire
// implementation in seqlock_host.c) -- a real round trip through
// compiled PARENA code, not a no-op passthrough. Returns the real POSIX
// fd and true on success; (0, false) on failure (open or flock error).
func Acquire(path string) (int, bool) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	fd := int(C.lock_acquire(cPath))
	if fd < 0 {
		return 0, false
	}
	return fd, true
}

// Release unlocks and closes fd (obtained from a successful Acquire),
// entering the PARENA-compiled mod's lock_release. Returns false on
// failure (unlock or close error) -- callers should treat this as
// diagnostic only, not retry-able, since the fd is closed either way.
func Release(fd int) bool {
	return C.lock_release(C.int(fd)) == 0
}
