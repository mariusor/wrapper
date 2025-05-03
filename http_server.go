package wrapper

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

func fileExists(dir string) bool {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return false
	}
	return true
}

type (
	c struct {
		s        http.Server
		h        http.Handler
		l        []net.Listener
		wTimeOut time.Duration
		cancelFn func()
		cert     string
		key      string
	}
	SetFn func(*c) error
)

func nilErrFn(_ string, _ ...interface{}) {}

func WriteWait(d time.Duration) SetFn {
	return func(c *c) error {
		c.wTimeOut = d
		return nil
	}
}

func OnTCP(addr string) SetFn {
	return func(c *c) error {
		if addr == "" {
			addr = ":http"
			if len(c.key)+len(c.cert) > 0 {
				addr = ":https"
			}
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		c.l = append(c.l, l)
		return nil
	}
}

func WithTLSCert(cert, key string) SetFn {
	if !fileExists(cert) {
		return func(*c) error { return fmt.Errorf("invalid certificate file %q", cert) }
	}
	if !fileExists(key) {
		return func(*c) error { return fmt.Errorf("invalid key file %q", key) }
	}
	return func(c *c) error {
		c.key = key
		c.cert = cert
		return nil
	}
}

func Handler(h http.Handler) SetFn {
	return func(c *c) error {
		c.h = h
		return nil
	}
}

func OnSocket(s string) SetFn {
	return func(c *c) error {
		l, err := net.Listen("unix", s)
		if err != nil {
			return err
		}
		c.l = append(c.l, l)
		return nil
	}
}

func OnSystemd() SetFn {
	nfds, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || nfds <= 0 {
		return func(_ *c) error {
			return fmt.Errorf("it appears that we're not expected to wait for a systemd socket connection")
		}
	}
	start := uintptr(3) // man 3 sd_listen_fds
	if fdStart, err := strconv.ParseInt(os.Getenv("SD_LISTEN_FDS_START"), 10, 32); err == nil {
		start = uintptr(fdStart)
	}
	return func(c *c) error {
		for i := start; i < uintptr(nfds); i++ {
			ff := os.NewFile(i, "Systemd listen fd")
			fi, err := ff.Stat()
			if err != nil {
				return err
			}
			if fi.Mode()&os.ModeSocket != os.ModeSocket {
				return fmt.Errorf("it appears that we're not expected to wait for a systemd socket connection")
			}
			l, err := net.FileListener(ff)
			if err != nil {
				return err
			}
			c.l = append(c.l, l)
		}
		return nil
	}
}

var (
	defaultTLSConfig = tls.Config{
		MinVersion:       tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}
	errStartFn = func(err error) func(context.Context) error {
		return func(context.Context) error {
			return err
		}
	}
	emptyStopFn = func(ctx context.Context) error {
		return nil
	}
)

type listenChan struct {
	l net.Listener
	e error
}

func (c *c) initServer(errChan chan listenChan) {
	for _, l := range c.l {
		if l == nil {
			continue
		}
		go func(ch chan<- listenChan, srv *http.Server, l net.Listener) {
			if len(c.cert)+len(c.key) > 0 {
				err := srv.ServeTLS(l, c.cert, c.key)
				ch <- listenChan{l: l, e: err}
			} else {
				err := srv.Serve(l)
				ch <- listenChan{l: l, e: err}
			}
		}(errChan, &c.s, l)
	}
}

func (c *c) start(ctx context.Context) error {
	errChan := make(chan listenChan, len(c.l))
	c.initServer(errChan)

	ongoingCtx, cancelFn := context.WithCancel(ctx)
	c.s.BaseContext = func(_ net.Listener) context.Context {
		return ongoingCtx
	}
	c.cancelFn = cancelFn

	errs := make([]error, 0, len(c.l))
	for i := 0; i < len(c.l); i++ {
		select {
		case <-ongoingCtx.Done():
		case stoppedCh := <-errChan:
			if !errors.Is(stoppedCh.e, http.ErrServerClosed) {
				errs = append(errs, fmt.Errorf("received error from %s: %w", stoppedCh.l.Addr(), stoppedCh.e))
			}
			continue
		}
	}
	return errors.Join(errs...)
}

func (c *c) stop(ctx context.Context) error {
	if c.cancelFn != nil {
		c.cancelFn()
	}

	if err := c.s.Shutdown(ctx); err != nil {
		return err
	}

	return nil
}

// HttpServer initializes an http.Server object with values set using SetFn() functions
func HttpServer(setters ...SetFn) (func(context.Context) error, func(context.Context) error) {
	c := c{
		l: make([]net.Listener, 0),
	}
	for _, fn := range setters {
		if err := fn(&c); err != nil {
			return errStartFn(err), emptyStopFn
		}
	}
	if len(c.l) == 0 {
		return errStartFn(fmt.Errorf("no listeners have been configured")), emptyStopFn
	}
	if c.h == nil {
		return errStartFn(fmt.Errorf("no handler has been configured")), emptyStopFn
	}
	c.s = http.Server{
		Handler:      c.h,
		TLSConfig:    &defaultTLSConfig,
		WriteTimeout: c.wTimeOut,
	}

	return c.start, c.stop
}
