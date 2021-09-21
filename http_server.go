package wrapper

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
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
	if !fileExists(key){
		return func(*c) error { return fmt.Errorf("invalid key file %q", key) }
	}
	return func(c *c) (err error) {
		if addr == "" {
			addr = ":https"
		}
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

func Socket() SetFn {
	return func (c *c) (err error) {
		c.l, err = net.FileListener(os.NewFile(3, "from systemd"))
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
	defaultRunFn = func() error {
		return nil
	}
)

// HttpServer initializes a http.Server object with values set using SetFn() functions
func HttpServer(ctx context.Context, setters ...SetFn) (func() error, func() error) {
	c := new(c)
	var serveFn, stopFn func() error
	for _, fn := range setters {
		if err := fn(c); err != nil {
			serveFn = func() error { return err }
		}
		stopFn = defaultRunFn
	}
	if c.l == nil {
		serveFn = func() error { return fmt.Errorf("no listeners have been configured") }
	}

	srv := &http.Server{
		Handler:      c.h,
		Addr:         c.addr,
		WriteTimeout: c.wTimeOut,
	}
	switch c.l.(type) {
	case *net.UnixListener:
		serveFn = func() error {
			return srv.Serve(c.l)
		}
	case *net.TCPListener:
		if len(c.cert) > 0 && len(c.key) > 0{
			serveFn = func() error {
				return srv.ServeTLS(c.l, c.cert, c.key)
			}
		} else {
			serveFn = func() error {
				return srv.Serve(c.l)
			}
		}
	}
	stopFn = func() error {
		return c.l.Close()
	}

	stop := func() error {
		if err := srv.Shutdown(ctx); err != nil {
			return err
		}
		if err := stopFn(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// Run our server in a goroutine so that it doesn't block.
	return serveFn, stop
}
