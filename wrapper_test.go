package wrapper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"
	"time"
)

type Writer struct {
	mu   sync.RWMutex
	buff bytes.Buffer
}

func (w *Writer) String() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.buff.String()
}

func (w *Writer) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buff.Write(p)
}

var out = new(Writer)

func loopPrintSeconds(w io.Writer) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		st := time.Now().Truncate(time.Second)
		tick := time.NewTicker(time.Second)
		for {
			select {
			case <-ctx.Done():
				if err := ctx.Err(); err != nil {
					_, _ = fmt.Fprintf(w, "Stopping\n")
					return err
				}
			case now := <-tick.C:
				_, _ = fmt.Fprintf(w, "%s", now.Sub(st).Truncate(time.Second))
			default:
			}
		}
	}
}

func sendSignals(pid int) {
	time.Sleep(1100 * time.Millisecond)
	_ = syscall.Kill(pid, syscall.SIGHUP)
	time.Sleep(1 * time.Second)
	_ = syscall.Kill(pid, syscall.SIGUSR1)
	time.Sleep(1 * time.Second)
	_ = syscall.Kill(pid, syscall.SIGTERM)
	time.Sleep(1 * time.Second)
	_ = syscall.Kill(pid, syscall.SIGINT)
}

func ExampleRegisterSignalHandlers() {
	l := log.New(out, "", 0)

	ctx, stopFn := context.WithTimeout(context.Background(), 8*time.Second)
	defer stopFn()

	go sendSignals(os.Getpid())

	err := RegisterSignalHandlers(
		SignalHandlers{
			syscall.SIGHUP: func(_ chan<- error) {
				l.SetPrefix("\nSIGHUP ")
				l.Printf("reloading config")
			},
			syscall.SIGUSR1: func(_ chan<- error) {
				l.SetPrefix("\nSIGUSR1 ")
				l.Printf("performing maintenance task")
			},
			syscall.SIGTERM: func(exit chan<- error) {
				l.SetPrefix("\nSIGTERM ")
				l.Printf("stopping gracefully")
				_, _ = fmt.Fprintf(l.Writer(), "Here we can gracefully close things (waiting 3s)\n")
				time.Sleep(3 * time.Second)
				exit <- nil
			},
			syscall.SIGINT: func(exit chan<- error) {
				exit <- fmt.Errorf("stopped with interruption")
			},
		},
	).Exec(ctx, loopPrintSeconds(out))

	if err != nil {
		l.Printf("%+s", err)
	}
	fmt.Printf(out.String())

	// Output:
	// 1s
	// SIGHUP reloading config
	// 2s
	// SIGUSR1 performing maintenance task
	// 3s
	// SIGTERM stopping gracefully
	// Here we can gracefully close things (waiting 3s)
	// 4s5s6s
}
