package infra

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"go.uber.org/zap"
)

type Request struct {
	Request      *http.Request
	Response     *http.Response
	RequestBody  []byte // hold the body because the actual body was closed
	ResponseBody []byte
}

type Proxy struct {
	name       string
	targetName string
	requests   chan Request
	proxy      *httputil.ReverseProxy
	server     *http.Server
}

func NewObservableProxy(name, targetName string, target *url.URL, port string) (*Proxy, chan Request) {
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
	}

	p := &Proxy{
		name:       name,
		targetName: targetName,
		requests:   make(chan Request),
		proxy:      proxy,
	}

	p.server = &http.Server{
		Addr:    port,
		Handler: p.handler(),
	}

	go func() {
		zap.S().Infow("starting observable proxy", "name", name, "target", targetName, "port", port)
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zap.S().Errorw("proxy server error", "name", name, "error", err)
		}
	}()

	return p, p.requests
}

func NewProxy(name, targetName string, target *url.URL, port string) *Proxy {
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
	}

	p := &Proxy{
		name:       name,
		targetName: targetName,
		proxy:      proxy,
	}

	p.server = &http.Server{
		Addr:    port,
		Handler: p.handler(),
	}

	go func() {
		zap.S().Infow("starting proxy", "name", name, "target", targetName, "port", port)
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zap.S().Errorw("proxy server error", "name", name, "error", err)
		}
	}()

	return p
}

func (p *Proxy) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody []byte
		if r.Body != nil {
			requestBody, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(requestBody))
		}

		clonedReq := r.Clone(r.Context())
		clonedReq.Body = io.NopCloser(bytes.NewReader(requestBody))

		recorder := &responseRecorder{
			ResponseWriter: w,
			body:           &bytes.Buffer{},
			statusCode:     http.StatusOK,
		}

		p.proxy.ServeHTTP(recorder, r)

		zap.S().Infow("proxy",
			"name", p.name,
			"target", p.targetName,
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.statusCode,
			"request_body", string(requestBody),
			"response_body", string(recorder.body.Bytes()),
		)

		if p.requests == nil {
			return
		}

		p.requests <- Request{
			Request:      clonedReq,
			RequestBody:  requestBody,
			Response:     recorder.toResponse(clonedReq),
			ResponseBody: recorder.body.Bytes(),
		}
	})
}

func (p *Proxy) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if p.server != nil {
		_ = p.server.Shutdown(ctx)
	}
	if p.requests != nil {
		close(p.requests)
	}
}

type responseRecorder struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *responseRecorder) toResponse(req *http.Request) *http.Response {
	return &http.Response{
		Status:     http.StatusText(r.statusCode),
		StatusCode: r.statusCode,
		Proto:      req.Proto,
		ProtoMajor: req.ProtoMajor,
		ProtoMinor: req.ProtoMinor,
		Header:     r.Header().Clone(),
		Body:       io.NopCloser(bytes.NewReader(r.body.Bytes())),
		Request:    req,
	}
}
