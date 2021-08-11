package wrapper

import (
	"context"
	"crypto/tls"
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
		wTimeOut time.Duration
		cert     string
		key      string
		addr     string
	}
	SetFn func(*c)
)

func WriteWait(d time.Duration) SetFn {
	return func(c *c) {
		c.wTimeOut = d
	}
}

func ListenOn(addr string) SetFn {
	return func(c *c) {
		c.addr = addr
	}
}

func SSL(cert, key string) SetFn {
	if !fileExists(cert) || !fileExists(key) {
		return func(*c) {}
	}
	return func(c *c) {
		c.cert = cert
		c.key = key
	}
}

func Handler(h http.Handler) SetFn {
	return func(c *c) {
		c.h = h
	}
}

// HttpServer initializes a http.Server object with values set using SetFn() functions
func HttpServer(ctx context.Context, setters ...SetFn) (func() error, func() error) {
	c := new(c)
	for _, fn := range setters {
		fn(c)
	}
	var serveFn func() error

	srv := &http.Server{
		Handler:      c.h,
		Addr:         c.addr,
		WriteTimeout: c.wTimeOut,
	}
	if fileExists(c.key) && fileExists(c.cert) {
		srv.TLSConfig = &tls.Config{
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
		serveFn = func() error {
			return srv.ListenAndServeTLS(c.cert, c.key)
		}
	} else {
		serveFn = func() error {
			return srv.ListenAndServe()
		}
	}

	stop := func() error {
		if err := srv.Shutdown(ctx); err != nil {
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
