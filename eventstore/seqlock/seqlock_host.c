/* seqlock_host.c -- real implementation of the two functions
 * stdlib/eventstore/seqlock.prn's #target bodies call into
 * (eventstore_seqlock_acquire/_release). Real OS-level advisory file
 * locking (flock(2), LOCK_EX/LOCK_UN) around a dedicated lock file, so
 * only one process across the whole system can hold it at a time --
 * the actual missing cross-process mutual exclusion that let
 * eventstore.FileStore.Append's in-memory, in-process-only sequence
 * counter collide across the ~15 separate Go binaries that all append
 * to the same var/secwatch store. See EMILY/BACKLOG.md SECTION for the
 * full root-cause writeup (sequence 112200 confirmed live with 5
 * different records from 2 different processes).
 *
 * flock, not fcntl byte-range locking: this lock always covers "the
 * whole critical section for this store," never a sub-range, so
 * flock's simpler whole-file semantics are the right tool -- fcntl
 * locks would add real complexity (per-process, per-fd-table
 * inheritance quirks) for no benefit here.
 *
 * O_CREAT so the very first process to reach this code creates the
 * lock file rather than requiring it to pre-exist; 0644 matches this
 * repo's own existing file-permission convention elsewhere in
 * eventstore (see file_store.go's own os.WriteFile/os.OpenFile calls).
 */
#include <fcntl.h>
#include <sys/file.h>
#include <unistd.h>
#include <errno.h>

int eventstore_seqlock_acquire(const char *path) {
    int fd = open(path, O_CREAT | O_RDWR, 0644);
    if (fd < 0) {
        return -1;
    }
    /* LOCK_EX blocks until the lock is available -- correct here: every
     * caller genuinely needs to wait its turn for the critical section,
     * not fail fast and retry (that would just move the race elsewhere). */
    while (flock(fd, LOCK_EX) != 0) {
        if (errno == EINTR) {
            continue; /* interrupted by a signal -- real, retry, not an error */
        }
        close(fd);
        return -1;
    }
    return fd;
}

int eventstore_seqlock_release(int fd) {
    if (fd < 0) {
        return -1; /* defensive: safe no-op-shaped failure on an already-failed acquire */
    }
    int unlock_result = flock(fd, LOCK_UN);
    int close_result = close(fd);
    if (unlock_result != 0 || close_result != 0) {
        return -1;
    }
    return 0;
}
