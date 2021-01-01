package wrapper

import (
	"os"
	"os/signal"
)

type (
	w struct {
		// signal is a channel which is waiting on user/os signals
		signal chan os.Signal
		// status is a channel on which we return the exit codes for application
		status chan int
		// handlers is the mapping of signals to functions to execute
		h SignalHandlers
	}

	handlerFn func(chan int)

	// SignalHandlers is a map that stores the association between signals and functions to be executed
	SignalHandlers map[os.Signal]handlerFn
)

// RegisterSignalHandlers sets up the signal handlers we want to use
func RegisterSignalHandlers(handlers SignalHandlers) *w {
	x := &w{
		signal: make(chan os.Signal, 1),
		status: make(chan int, 1),
		h:      handlers,
	}
	signals := make([]os.Signal, 0)
	for sig := range handlers {
		signals = append(signals, sig)
	}
	signal.Notify(x.signal, signals...)
	return x
}

// Exec reads signals received from the os and executes the handlers it has registered
func (ww *w) Exec(fn func()) int {
	go fn()
	go func(ex *w) {
		for {
			select {
			case s := <-ex.signal:
				ex.h[s](ex.status)
			}
		}
	}(ww)
	return <- ww.status
}
