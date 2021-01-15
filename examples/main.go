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

const (
	RunTime  = 6 * time.Second
	WaitTime = 400 * time.Millisecond
)

func wait() error {
	ctx, _ := context.WithTimeout(context.Background(), RunTime)
	var err error

	go func (err error) {
		select {
		case <- ctx.Done():
			err = ctx.Err()
			fmt.Printf("Stopping\n")
		}
	}(err)
	if err != nil {
		return err
	}
	for {
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
	return nil
}

func main() {
	l := log.New(os.Stdout, "", 0)
	os.Exit(wrapper.RegisterSignalHandlers(
		wrapper.SignalHandlers{
			syscall.SIGHUP: func(_ chan int) {
				fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGHUP ")
				l.Printf("reloading config")
			},
			syscall.SIGUSR1: func(_ chan int) {
				fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGUSR1 ")
				l.Printf("performing maintenance task #1")
			},
			syscall.SIGUSR2: func(_ chan int) {
				fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGUSR2 ")
				l.Printf("performing maintenance task #2")
			},
			syscall.SIGTERM: func(exit chan int) {
				// kill -SIGTERM XXXX
				fmt.Fprintln(l.Writer())
				l.SetPrefix("SIGTERM ")
				l.Printf("stopping")
				exit <- 0
			},
			syscall.SIGINT: func(exit chan int) {
				// kill -SIGINT XXXX or Ctrl+c
				l.SetPrefix("SIGINT ")
				fmt.Fprintln(l.Writer())
				l.Printf("stopping gracefully")
				fmt.Fprintf(l.Writer(), "\nHere we can gracefully close things (waiting 3s)\n")
				time.Sleep(3*time.Second)
				exit <- 0
				fmt.Fprintln(l.Writer())
			},
			syscall.SIGQUIT: func(exit chan int) {
				l.SetPrefix("SIGQUIT ")
				l.SetOutput(os.Stderr)
				fmt.Fprintln(l.Writer())
				l.Printf("force stopping")
				exit <- -1
			},
		},
	).Exec(wait))
}
