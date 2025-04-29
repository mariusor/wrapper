package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"syscall"
	"time"

	"git.sr.ht/~mariusor/wrapper"
)

const WaitTime = 200 * time.Millisecond

func printSpinner() {
	fmt.Printf("waiting")
	time.Sleep(WaitTime)
	fmt.Printf(".")
	time.Sleep(WaitTime)
	fmt.Printf(".")
	time.Sleep(WaitTime)
	fmt.Printf(".")
	time.Sleep(WaitTime)
	fmt.Printf("\r")
}

func wait(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("Stopping\n")
			if err := ctx.Err(); err != nil {
				return err
			}
		default:
			printSpinner()
		}
	}
	return nil
}

func main() {
	l := log.New(os.Stdout, "", 0)

	stopGracefully := func(err error) error {
		_, _ = fmt.Fprintln(l.Writer())
		l.Printf("stopping gracefully")
		_, _ = fmt.Fprintf(l.Writer(), "\nHere we can gracefully close things (waiting 3s)\n")
		time.Sleep(3 * time.Second)
		return err
	}

	err := wrapper.RegisterSignalHandlers(
		wrapper.SignalHandlers{
			syscall.SIGHUP: func(_ chan<- error) {
				_, _ = fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGHUP ")
				l.Printf("reloading config")
			},
			syscall.SIGUSR1: func(_ chan<- error) {
				_, _ = fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGUSR1 ")
				l.Printf("performing maintenance task #1")
			},
			syscall.SIGUSR2: func(_ chan<- error) {
				_, _ = fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGUSR2 ")
				l.Printf("performing maintenance task #2")
			},
			syscall.SIGTERM: func(exit chan<- error) {
				// kill -SIGTERM XXXX
				_, _ = fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGTERM ")
				exit <- stopGracefully(nil)
			},
			syscall.SIGINT: func(exit chan<- error) {
				// kill -SIGINT XXXX or Ctrl+c
				l.SetPrefix("SIGINT ")
				exit <- stopGracefully(fmt.Errorf("Interrupted"))
			},
			syscall.SIGQUIT: func(exit chan<- error) {
				l.SetPrefix("SIGQUIT ")
				l.SetOutput(os.Stderr)
				_, _ = fmt.Fprintln(l.Writer())
				l.Printf("force stopping")
				exit <- fmt.Errorf("forced stop")
			},
		},
	).Exec(context.Background(), wait)

	if err != nil {
		l.Printf("%+s", err)
		os.Exit(-1)
	}
}
