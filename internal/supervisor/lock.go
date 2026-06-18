package supervisor

import (
	"os"
	"syscall"
)

// Lock is a single-flight advisory lock. If a tick runs long and the next
// launchd fire starts a second supervisor, the second one fails to acquire and
// exits, so two ticks can never evaluate (and double-fire) concurrently.
type Lock struct{ f *os.File }

// AcquireLock takes a non-blocking exclusive flock on path. ok=false means
// another supervisor already holds it (the caller should just exit).
func AcquireLock(path string) (lock *Lock, ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &Lock{f: f}, true, nil
}

func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}
