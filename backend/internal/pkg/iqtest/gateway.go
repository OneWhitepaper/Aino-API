package iqtest

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
)

// GatewayTransport dispatches only our fixed Responses/models paths through the
// fully configured normal gateway, without exposing a listening loopback proxy.
type GatewayTransport struct {
	mu      sync.RWMutex
	handler http.Handler
}

func NewGatewayTransport() *GatewayTransport { return &GatewayTransport{} }
func (t *GatewayTransport) SetHandler(handler http.Handler) {
	t.mu.Lock()
	t.handler = handler
	t.mu.Unlock()
}

type clientIPKey struct{}

func WithClientIP(req *http.Request, ip string) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), clientIPKey{}, ip))
}

func (t *GatewayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.RLock()
	handler := t.handler
	t.mu.RUnlock()
	allowed := req.URL.Host == "aino.internal" && req.URL.Scheme == "http" && req.URL.RawQuery == "" && ((req.Method == http.MethodPost && req.URL.Path == "/v1/responses") || (req.Method == http.MethodGet && req.URL.Path == "/v1/models"))
	clientIP, _ := req.Context().Value(clientIPKey{}).(string)
	if !allowed || handler == nil || net.ParseIP(clientIP) == nil {
		return nil, errors.New("gateway_unavailable")
	}
	ctx, cancel := context.WithCancel(req.Context())
	request := req.Clone(ctx)
	request.RequestURI = request.URL.RequestURI()
	request.RemoteAddr = net.JoinHostPort(clientIP, "0")
	request.Header = make(http.Header)
	for _, name := range []string{"Authorization", "Content-Type", "Accept"} {
		if value := req.Header.Get(name); value != "" {
			request.Header.Set(name, value)
		}
	}
	request.Header.Set("User-Agent", "Aino-IQ-Test/1.0")
	reader, writer := io.Pipe()
	w := &gatewayWriter{header: make(http.Header), writer: writer, ready: make(chan struct{}), closed: make(chan bool)}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = reader.CloseWithError(ctx.Err())
			_ = writer.CloseWithError(ctx.Err())
			close(w.closed)
		case <-done:
		}
	}()
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				_ = writer.CloseWithError(errors.New("gateway_failed"))
			}
			w.commit(http.StatusOK)
			_ = writer.Close()
		}()
		handler.ServeHTTP(w, request)
	}()
	select {
	case <-w.ready:
		return &http.Response{StatusCode: w.status, Header: w.responseHeader, Body: &gatewayBody{ReadCloser: reader, cancel: cancel}, Request: req}, nil
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
}

type gatewayBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *gatewayBody) Close() error { b.cancel(); return b.ReadCloser.Close() }

type gatewayWriter struct {
	header         http.Header
	responseHeader http.Header
	writer         *io.PipeWriter
	ready          chan struct{}
	closed         chan bool
	status         int
}

func (w *gatewayWriter) Header() http.Header { return w.header }
func (w *gatewayWriter) commit(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.responseHeader = w.header.Clone()
	close(w.ready)
}
func (w *gatewayWriter) WriteHeader(status int) { w.commit(status) }
func (w *gatewayWriter) Write(body []byte) (int, error) {
	w.commit(http.StatusOK)
	return w.writer.Write(body)
}
func (w *gatewayWriter) Flush()                   { w.commit(http.StatusOK) }
func (w *gatewayWriter) CloseNotify() <-chan bool { return w.closed }
