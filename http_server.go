package wrapper

import (
	"context"
	"crypto/tls"
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
		h        http.Handler
		l        []net.Listener
		s        []http.Server
		wTimeOut time.Duration
		cert     string
		key      string
		errFn    func(s string, p ...interface{})
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

func HTTP(addr string) SetFn {
	return func(c *c) error {
		if addr == "" {
			addr = ":http"
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		c.l = append(c.l, l)
		return nil
	}
}

func HTTPS(addr, cert, key string) SetFn {
	if !fileExists(cert) {
		return func(*c) error { return fmt.Errorf("invalid certificate file %q", cert) }
	}
	if !fileExists(key) {
		return func(*c) error { return fmt.Errorf("invalid key file %q", key) }
	}
	return func(c *c) error {
		if addr == "" {
			addr = ":https"
		}
		c.key = key
		c.cert = cert
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		c.l = append(c.l, l)
		return nil
	}
}

func Handler(h http.Handler) SetFn {
	return func(c *c) error {
		c.h = h
		return nil
	}
}

func Socket(s string) SetFn {
	return func(c *c) error {
		l, err := net.Listen("unix", s)
		if err != nil {
			return err
		}
		c.l = append(c.l, l)
		return nil
	}
}

func Systemd() SetFn {
	nfds, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || nfds == 0 {
		return func(_ *c) error {
			return fmt.Errorf("it appears that we're not expected to wait for a systemd socket connection")
		}
	}
	return func(c *c) error {
		l, err := net.FileListener(os.NewFile(3, "Systemd listen fd"))
		if err != nil {
			return err
		}
		c.l = append(c.l, l)
		return nil
	}
}

func Err(errFn func(s string, p ...interface{})) SetFn {
	return func(c *c) error {
		c.errFn = errFn
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
	errStartFn = func(err error) func() error {
		return func() error {
			return err
		}
	}
	emptyStopFn = func(ctx context.Context) error {
		return nil
	}
)

func (c *c) start() error {
	c.s = make([]http.Server, 0, len(c.l))
	errChan := make(chan error, len(c.l))
	for _, l := range c.l {
		srv := http.Server{
			Handler:      c.h,
			Addr:         l.Addr().String(),
			WriteTimeout: c.wTimeOut,
		}
		c.s = append(c.s, srv)
		switch l.(type) {
		case *net.UnixListener:
			go func() {
				errChan <- srv.Serve(l)
			}()
		case *net.TCPListener:
			if len(c.cert) > 0 && len(c.key) > 0 {
				go func() {
					errChan <- srv.ServeTLS(l, c.cert, c.key)
				}()
			} else {
				go func() {
					errChan <- srv.Serve(l)
				}()
			}
		}
	}
	select {
	case err := <-errChan:
		return err
	}
	return nil
}

func (c *c) stop(ctx context.Context) error {
	var err error
	for i, l := range c.l {
		l.Close()
		srv := c.s[i]
		if err := srv.Shutdown(ctx); err != nil {
			return err
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	}
	return err
}

// HttpServer initializes a http.Server object with values set using SetFn() functions
func HttpServer(setters ...SetFn) (func() error, func(context.Context) error) {
	c := c{
		l:     make([]net.Listener, 0),
		errFn: nilErrFn,
	}
	for _, fn := range setters {
		if err := fn(&c); err != nil {
			return errStartFn(err), emptyStopFn
		}
	}
	if c.l == nil {
		return errStartFn(fmt.Errorf("no listeners have been configured")), emptyStopFn
	}

	return c.start, c.stop
}
