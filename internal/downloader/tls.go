package downloader

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
	"golang.org/x/sync/singleflight"
)

// chromeHello is the handshake to imitate. Cloudflare fingerprints the
// ClientHello, and Go's offers a cipher and extension set no browser sends, so
// a spoofed User-Agent alone contradicts it.
var chromeHello = utls.HelloChrome_Auto

// browserTransport sends every request behind chromeHello.
//
// ALPN offers h2 and http/1.1 as Chrome does, and which one a host picked is
// known only after the handshake. Neither net/http nor http2.Transport will
// hand a connection to the other, so the first request to a host spends one
// throwaway handshake learning the answer, and later ones pool normally.
type browserTransport struct {
	dialer *net.Dialer
	h1     *http.Transport
	h2     *http2.Transport

	probes singleflight.Group // collapses concurrent first requests to a host

	mu   sync.Mutex
	alpn map[string]string // host:port -> negotiated protocol
}

// newBrowserTransport builds the transport shared by every request in a run.
//
// There is deliberately no Client.Timeout above this: it bounds the body read
// too, which would kill long audio downloads mid-stream. The bounds below cover
// connection setup and waiting for headers, which is what needs a deadline.
func newBrowserTransport() *browserTransport {
	t := &browserTransport{
		dialer: &net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		},
		alpn: make(map[string]string),
	}

	t.h1 = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           t.dialer.DialContext,
		DialTLSContext:        t.dialTLS,
		MaxIdleConns:          maxConnections,
		MaxIdleConnsPerHost:   maxConnections,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	t.h2 = &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return t.dialTLS(ctx, network, addr)
		},
		ReadIdleTimeout: 30 * time.Second,
		PingTimeout:     15 * time.Second,
	}
	return t
}

func (t *browserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return t.h1.RoundTrip(req)
	}

	proto, err := t.protocol(req.Context(), authority(req.URL))
	if err != nil {
		return nil, err
	}
	if proto == http2.NextProtoTLS {
		return t.h2.RoundTrip(req)
	}
	return t.h1.RoundTrip(req)
}

// CloseIdleConnections lets http.Client's cleanup reach both transports.
func (t *browserTransport) CloseIdleConnections() {
	t.h1.CloseIdleConnections()
	t.h2.CloseIdleConnections()
}

// protocol reports the ALPN protocol addr negotiates, remembering it for the run.
func (t *browserTransport) protocol(ctx context.Context, addr string) (string, error) {
	t.mu.Lock()
	proto, ok := t.alpn[addr]
	t.mu.Unlock()
	if ok {
		return proto, nil
	}

	v, err, _ := t.probes.Do(addr, func() (any, error) {
		conn, err := t.dialTLS(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		proto := conn.(*utls.UConn).ConnectionState().NegotiatedProtocol
		conn.Close()

		t.mu.Lock()
		t.alpn[addr] = proto
		t.mu.Unlock()
		return proto, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (t *browserTransport) dialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	raw, err := t.dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		raw.Close()
		return nil, err
	}

	// ALPN and the rest of the extensions come from chromeHello; naming any
	// here would overwrite the fingerprint being imitated.
	conn := utls.UClient(raw, &utls.Config{ServerName: host}, chromeHello)
	if err := conn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, fmt.Errorf("TLS handshake with %s: %w", addr, err)
	}
	return conn, nil
}

// authority is the host:port a URL dials, filling in the port its scheme implies.
func authority(u *url.URL) string {
	if port := u.Port(); port != "" {
		return net.JoinHostPort(u.Hostname(), port)
	}
	if u.Scheme == "http" {
		return net.JoinHostPort(u.Hostname(), "80")
	}
	return net.JoinHostPort(u.Hostname(), "443")
}
