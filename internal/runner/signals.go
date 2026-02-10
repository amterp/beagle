package runner

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// ForwardSignals sets up signal forwarding from this process to the child.
// Returns a cleanup function that must be called after the child exits
// to stop the signal forwarding goroutine and release the channel.
func ForwardSignals(child *exec.Cmd) func() {
	sigs := make(chan os.Signal, 4)
	done := make(chan struct{})

	// Register before starting the goroutine to avoid missing signals
	// delivered between child.Start() and the goroutine running.
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for {
			select {
			case s, ok := <-sigs:
				if !ok {
					return
				}
				if child.Process != nil {
					_ = child.Process.Signal(s)
				}
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(sigs)
		close(done)
	}
}
