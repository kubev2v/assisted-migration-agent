package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"

	"go.uber.org/zap"
)

type Request struct {
	Request      *http.Request
	Response     *http.Response
	RequestBody  []byte // hold the body because the actual body was closed
	ResponseBody []byte
}

type Proxy struct {
	requests chan Request
	proxy    *httputil.ReverseProxy
}

func NewProxy(target *url.URL) (*Proxy, chan Request) {
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
	}

	p := &Proxy{
		requests: make(chan Request),
		proxy:    proxy,
	}

	return p, p.requests
}

func (p *Proxy) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zap.S().Infow("proxy request", "method", r.Method, "path", r.URL.Path)

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

		zap.S().Infow("proxy response", "method", r.Method, "path", r.URL.Path, "status", recorder.statusCode)

		zap.S().Infow("request body", "body", string(requestBody))
		p.requests <- Request{
			Request:      clonedReq,
			RequestBody:  requestBody,
			Response:     recorder.toResponse(clonedReq),
			ResponseBody: recorder.body.Bytes(),
		}
	})
}

func (p *Proxy) Close() {
	close(p.requests)
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
