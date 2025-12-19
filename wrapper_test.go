package wrapper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/goleak"
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
					_, _ = fmt.Fprintf(w, "done: %s\n", err)
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
				l.SetPrefix("\nSIGINT ")
				l.Printf("interrupted by user\n")
				exit <- Interrupt
			},
		},
	).Exec(ctx, loopPrintSeconds(out))

	if err != nil {
		l.Printf("%s", err)
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
	// 4s
	// SIGINT interrupted by user
	// done: context canceled
	//
}

func Test_w_Exec(t *testing.T) {
	handleErrFn := func(err error) func(errors chan<- error) {
		return func(errors chan<- error) {
			errors <- err
		}
	}

	nilExecFn := func(ctx context.Context) error { return nil }

	type fields struct {
		signal chan os.Signal
		err    chan error
		h      SignalHandlers
	}
	type args struct {
		ctx context.Context
		fn  func(context.Context) error
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name:    "empty",
			fields:  fields{},
			args:    args{},
			wantErr: nil,
		},
		{
			name: "no signal",
			fields: fields{
				signal: make(chan os.Signal),
				err:    make(chan error),
				h: SignalHandlers{
					syscall.SIGHUP: handleErrFn(nil),
				},
			},
			args:    args{context.TODO(), nilExecFn},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			ww := &w{
				signal: tt.fields.signal,
				err:    tt.fields.err,
				h:      tt.fields.h,
			}
			if err := ww.Exec(tt.args.ctx, tt.args.fn); !cmp.Equal(err, tt.wantErr) {
				t.Errorf("Exec() error = %s", cmp.Diff(err, tt.wantErr))
			}
		})
	}
}
