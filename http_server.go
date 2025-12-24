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
		gWait    time.Duration
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

func GracefulWait(g time.Duration) SetFn {
	return func(c *c) error {
		c.gWait = g
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
	defaultServer = http.Server{
		TLSConfig: &defaultTLSConfig,
	}
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

func (cc *c) initServer(errChan chan error) {
	for _, l := range cc.l {
		if l == nil {
			continue
		}
		go func(c *c, l net.Listener) {
			errFn := func(err error) error {
				if err == nil {
					return nil
				}
				return fmt.Errorf("error on listener %s: %w", l.Addr(), err)
			}

			if len(c.cert)+len(c.key) > 0 {
				errChan <- errFn(c.s.ServeTLS(l, c.cert, c.key))
			} else {
				errChan <- errFn(c.s.Serve(l))
			}
		}(cc, l)
	}
}

func initBaseContext(cc *c, ctx context.Context) {
	ongoingCtx, cancelFn := context.WithCancel(ctx)
	cc.s.BaseContext = func(_ net.Listener) context.Context {
		return ongoingCtx
	}
	cc.cancelFn = cancelFn
}

func (cc *c) start(ctx context.Context) error {
	errChan := make(chan error, len(cc.l))
	cc.initServer(errChan)

	// FIXME(marius): this triggers race conditions
	//initBaseContext(cc, ctx)

	errs := make([]error, 0, len(cc.l))
	for i := 0; i < len(cc.l); i++ {
		select {
		case err := <-errChan:
			if !errors.Is(err, http.ErrServerClosed) {
				errs = append(errs, fmt.Errorf("received error from: %w", err))
			}
			continue
		}
	}
	return errors.Join(errs...)
}

func (cc *c) stop(ctx context.Context) error {
	if cc.cancelFn != nil {
		defer cc.cancelFn()
	}

	if cc.gWait > 0 {
		// NOTE(marius): wait on wait timer to tick, or context to be canceled
		wait := time.NewTimer(cc.gWait)
		select {
		case <-ctx.Done():
		case <-wait.C:
		}
	}

	if err := cc.s.Shutdown(ctx); err != nil {
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
		TLSConfig: &defaultTLSConfig,
		Handler:   c.h,
	}
	if c.wTimeOut > 0 {
		c.s.WriteTimeout = c.wTimeOut
	}

	return c.start, c.stop
}
