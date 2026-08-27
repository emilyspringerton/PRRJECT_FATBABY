/* seqlock_host.h -- real extern declarations for the two host-side
 * symbols seqlock_mod.c's #target/inline-c bodies call into, plus the
 * mod's own entry points. Same "-include this header before compiling
 * the generated C" pattern PITVIPER's scrollmod_host.h and EmilyOS's
 * fsaclmod_host.h already established -- here wired in via cgo's
 * `#cgo CFLAGS: -include`, same mechanism, different repo.
 *
 * Unlike scrollmod_host.h's pitviper_host_scroll (a Go-exported
 * callback), eventstore_seqlock_acquire/_release are real, complete,
 * self-contained C implementations in seqlock_host.c -- no callback
 * into Go needed, this is a pure OS-syscall wrapper (open + flock +
 * close), the same class of "genuinely irreducible raw syscall" io.prn's
 * own raw_open_impl/raw_close_impl already are.
 */
#ifndef SEQLOCK_HOST_H
#define SEQLOCK_HOST_H

extern int eventstore_seqlock_acquire(const char *path);
extern int eventstore_seqlock_release(int fd);
extern int lock_acquire(char *path);
extern int lock_release(int fd);

#endif /* SEQLOCK_HOST_H */
