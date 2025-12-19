package wrapper

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type (
	w struct {
		// signal is a channel which is waiting on user/os signals
		signal chan os.Signal
		// err is a channel on which we return the error for application
		err chan error
		// handlers is the mapping of signals to functions to execute
		h SignalHandlers
		m sync.Mutex
	}

	handlerFn func(chan<- error)

	// SignalHandlers is a map that stores the association between signals and functions to be executed
	SignalHandlers map[os.Signal]handlerFn
)

func signals(handlers SignalHandlers) []os.Signal {
	handled := make([]os.Signal, 0, len(handlers))
	for sig := range handlers {
		handled = append(handled, sig)
	}
	return handled
}

var Interrupt = syscall.EINTR

// RegisterSignalHandlers sets up the signal handlers we want to use
func RegisterSignalHandlers(handlers SignalHandlers) *w {
	ww := &w{
		signal: make(chan os.Signal),
		err:    make(chan error, 1),
		h:      handlers,
	}
	signal.Notify(ww.signal, signals(handlers)...)
	return ww
}

func (ww *w) wait(ctx context.Context) {
	errCh := make(chan error, 1)
	defer close(errCh)
	for {
		select {
		case <-ctx.Done():
			//ww.err <- ctx.Err()
			return
		case err := <-errCh:
			if err != nil {
				if errors.Is(err, Interrupt) {
					err = nil
				}
				ww.err <- err
				return
			}
		case sig := <-ww.signal:
			if handler, ok := ww.h[sig]; ok {
				// NOTE(marius): run this asynchronously
				go handler(errCh)
			}
		}
	}
}

func (ww *w) exec(ctx context.Context, fn func(context.Context) error) {
	ww.err <- fn(ctx)
}

// Exec reads signals received from the os and executes the handlers it has registered for it
// The execution ends when fn finishes, or when any of the signal handler functions passes an error
// through the error channel.
func (ww *w) Exec(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var cancelFn func()
	ctx, cancelFn = context.WithCancel(ctx)

	defer func() {
		// NOTE(marius): cleanup
		cancelFn()
		signal.Stop(ww.signal)
	}()

	if fn == nil {
		return nil
	}

	// NOTE(marius): loop and wait for signals
	go ww.wait(ctx)

	// NOTE(marius): call the main execution function
	go ww.exec(ctx, fn)

	// NOTE(marius): blocking until an error is pushed through our error channel
	// It can come either from the main execution function, or from any of the registered signal handler functions.
	return <-ww.err
}
