package main

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"time"
	"wrapper"
)

func wait() {
	const _wait = 400*time.Millisecond
	for {
		fmt.Printf("waiting")
		time.Sleep(_wait)
		fmt.Printf(".")
		time.Sleep(_wait)
		fmt.Printf(".")
		time.Sleep(_wait)
		fmt.Printf(".")
		time.Sleep(_wait)
		fmt.Printf("\r")
	}
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
