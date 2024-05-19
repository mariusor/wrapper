package wrapper

import (
	"context"
	"os"
	"os/signal"
)

type (
	w struct {
		// signal is a channel which is waiting on user/os signals
		signal chan os.Signal
		// err is a channel on which we return the error for application
		err chan error
		// handlers is the mapping of signals to functions to execute
		h SignalHandlers
	}

	handlerFn func(chan<- error)

	// SignalHandlers is a map that stores the association between signals and functions to be executed
	SignalHandlers map[os.Signal]handlerFn
)

// RegisterSignalHandlers sets up the signal handlers we want to use
func RegisterSignalHandlers(handlers SignalHandlers) *w {
	x := &w{
		signal: make(chan os.Signal, 1),
		err:    make(chan error, 1),
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
func (ww *w) Exec(ctx context.Context, fn func(context.Context) error) error {
	go func() {
		if err := fn(ctx); err != nil {
			ww.err <- err
		}
	}()
	go func(ex *w) {
		for {
			select {
			case s := <-ex.signal:
				ex.h[s](ex.err)
			}
		}
	}(ww)
	return <-ww.err
}
