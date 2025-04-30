package wrapper

import (
	"context"
	"fmt"
	"log"
	"os"
	"syscall"
	"time"
)

func loopPrintSeconds(ctx context.Context) error {
	st := time.Now()
	for {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				fmt.Printf("Stopping\n")
				return err
			}
		default:
			time.Sleep(900 * time.Millisecond)
			fmt.Printf("%s", time.Now().Sub(st).Truncate(time.Second))
		}
	}
	return nil
}

func sendSignals(pid int) {
	time.Sleep(1 * time.Second)
	_ = syscall.Kill(pid, syscall.SIGHUP)
	time.Sleep(1 * time.Second)
	_ = syscall.Kill(pid, syscall.SIGUSR1)
	time.Sleep(1 * time.Second)
	_ = syscall.Kill(pid, syscall.SIGTERM)
	time.Sleep(2 * time.Second)
	_ = syscall.Kill(pid, syscall.SIGINT)
}

func ExampleRegisterSignalHandlers() {
	l := log.New(os.Stdout, "", 0)

	ctx, stopFn := context.WithTimeout(context.Background(), 7*time.Second)
	defer stopFn()

	go sendSignals(os.Getpid())

	err := RegisterSignalHandlers(
		SignalHandlers{
			syscall.SIGHUP: func(_ chan<- error) {
				_, _ = fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGHUP ")
				l.Printf("reloading config")
			},
			syscall.SIGUSR1: func(_ chan<- error) {
				_, _ = fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGUSR1 ")
				l.Printf("performing maintenance task")
			},
			syscall.SIGTERM: func(exit chan<- error) {
				// kill -SIGTERM XXXX
				_, _ = fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGTERM ")
				l.Printf("stopping gracefully")
				_, _ = fmt.Fprintf(l.Writer(), "Here we can gracefully close things (waiting 3s)\n")
				time.Sleep(3 * time.Second)
				exit <- nil
			},
			syscall.SIGINT: func(exit chan<- error) {
				_, _ = fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGINT ")
				l.Printf("interrupted by user")
				exit <- fmt.Errorf("stopped with intteruption")
			},
		},
	).Exec(ctx, loopPrintSeconds)

	if err != nil {
		l.Printf("%+s", err)
	}

	// Output:
	// 0s
	// SIGHUP reloading config
	// 1s
	// SIGUSR1 performing maintenance task
	// 2s
	// SIGTERM stopping gracefully
	// Here we can gracefully close things (waiting 3s)
	// 3s4s5s
	// SIGINT interrupted by user
}
