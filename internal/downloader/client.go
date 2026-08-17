package downloader

import (
	"net"
	"net/http"
	"time"
)

// NewClient builds the single client shared by every request in a run.
//
// There is deliberately no Client.Timeout: it bounds the whole request
// including the body read, which would kill long audio downloads mid-stream.
// The bounds below apply to connection setup and to waiting for headers, which
// is what actually needs a deadline.
func NewClient() *http.Client {
	t := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{Transport: t}
}
