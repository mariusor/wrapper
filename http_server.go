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
		l        net.Listener
		wTimeOut time.Duration
		cert     string
		key      string
		addr     string
	}
	SetFn func(*c) error
)

func WriteWait(d time.Duration) SetFn {
	return func(c *c) error {
		c.wTimeOut = d
		return nil
	}
}

func HTTP(addr string) SetFn {
	return func(c *c) (err error) {
		if addr == "" {
			addr = ":http"
		}
		c.addr = addr
		c.l, err = net.Listen("tcp", c.addr)
		return
	}
}

func HTTPS(addr, cert, key string) SetFn {
	if !fileExists(cert) {
		return func(*c) error { return fmt.Errorf("invalid certificate file %q", cert) }
	}
	if !fileExists(key) {
		return func(*c) error { return fmt.Errorf("invalid key file %q", key) }
	}
	return func(c *c) (err error) {
		if addr == "" {
			addr = ":https"
		}
		c.key = key
		c.cert = cert
		c.l, err = net.Listen("tcp", addr)
		return
	}
}

func Handler(h http.Handler) SetFn {
	return func(c *c) error {
		c.h = h
		return nil
	}
}

func Socket(s string) SetFn {
	return func(c *c) (err error) {
		c.l, err = net.Listen("unix", s)
		return
	}
}

func Systemd() SetFn {
	return func(c *c) (err error) {
		nfds, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
		if err != nil || nfds == 0 {
			return fmt.Errorf("it appears that we're not expected to wait for a systemd socket connection")
		}
		c.l, err = net.FileListener(os.NewFile(3, "Systemd listen fd"))
		return
	}
}

var (
	defaultTLSConfig = tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
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

// HttpServer initializes a http.Server object with values set using SetFn() functions
func HttpServer(setters ...SetFn) (func() error, func(context.Context) error) {
	c := new(c)
	var startFn func() error
	for _, fn := range setters {
		if err := fn(c); err != nil {
			return errStartFn(err), emptyStopFn
		}
	}
	if c.l == nil {
		return errStartFn(fmt.Errorf("no listeners have been configured")), emptyStopFn
	}

	srv := http.Server{
		Handler:      c.h,
		Addr:         c.addr,
		WriteTimeout: c.wTimeOut,
	}
	stopFn := func(ctx context.Context) error {
		if err := c.l.Close(); err != nil {
			return err
		}
		if err := srv.Shutdown(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	switch c.l.(type) {
	case *net.UnixListener:
		startFn = func() error {
			return srv.Serve(c.l)
		}
	case *net.TCPListener:
		if len(c.cert) > 0 && len(c.key) > 0 {
			startFn = func() error {
				return srv.ServeTLS(c.l, c.cert, c.key)
			}
		} else {
			startFn = func() error {
				return srv.Serve(c.l)
			}
		}
	}

	return startFn, stopFn
}
