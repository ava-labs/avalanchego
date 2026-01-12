// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ava-labs/avalanchego/proto/pb/io/reader"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/greader"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gresponsewriter"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
	responsewriterpb "github.com/ava-labs/avalanchego/proto/pb/http/responsewriter"
)

var (
	_ httppb.HTTPServer   = (*Server)(nil)
	_ http.ResponseWriter = (*ResponseWriter)(nil)
)

// Server is an http.Handler that is managed over RPC.
type Server struct {
	httppb.UnsafeHTTPServer
	handler http.Handler
}

// NewServer returns an http.Handler instance managed remotely
func NewServer(handler http.Handler) *Server {
	return &Server{
		handler: handler,
	}
}

func (s *Server) Handle(ctx context.Context, req *httppb.HTTPRequest) (*httppb.HTTPResponse, error) {
	clientConn, err := grpcutils.Dial(req.ResponseWriter.ServerAddr)
	if err != nil {
		return nil, err
	}

	writerHeaders := make(http.Header)
	for _, elem := range req.ResponseWriter.Header {
		writerHeaders[elem.Key] = elem.Values
	}

	writer := gresponsewriter.NewClient(writerHeaders, responsewriterpb.NewWriterClient(clientConn))
	body := greader.NewClient(reader.NewReaderClient(clientConn))

	// create the request with the current context
	request, err := http.NewRequestWithContext(
		ctx,
		req.Request.Method,
		req.Request.RequestUri,
		body,
	)
	if err != nil {
		return nil, err
	}

	if req.Request.Url != nil {
		request.URL = &url.URL{
			Scheme:     req.Request.Url.Scheme,
			Opaque:     req.Request.Url.Opaque,
			Host:       req.Request.Url.Host,
			Path:       req.Request.Url.Path,
			RawPath:    req.Request.Url.RawPath,
			ForceQuery: req.Request.Url.ForceQuery,
			RawQuery:   req.Request.Url.RawQuery,
			Fragment:   req.Request.Url.Fragment,
		}
		if req.Request.Url.User != nil {
			if req.Request.Url.User.PasswordSet {
				request.URL.User = url.UserPassword(req.Request.Url.User.Username, req.Request.Url.User.Password)
			} else {
				request.URL.User = url.User(req.Request.Url.User.Username)
			}
		}
	}

	request.Proto = req.Request.Proto
	request.ProtoMajor = int(req.Request.ProtoMajor)
	request.ProtoMinor = int(req.Request.ProtoMinor)
	request.Header = make(http.Header, len(req.Request.Header))
	for _, elem := range req.Request.Header {
		request.Header[elem.Key] = elem.Values
	}
	request.ContentLength = req.Request.ContentLength
	request.TransferEncoding = req.Request.TransferEncoding
	request.Host = req.Request.Host
	request.Form = make(url.Values, len(req.Request.Form))
	for _, elem := range req.Request.Form {
		request.Form[elem.Key] = elem.Values
	}
	request.PostForm = make(url.Values, len(req.Request.PostForm))
	for _, elem := range req.Request.PostForm {
		request.PostForm[elem.Key] = elem.Values
	}
	request.Trailer = make(http.Header)
	request.RemoteAddr = req.Request.RemoteAddr
	request.RequestURI = req.Request.RequestUri

	if req.Request.Tls != nil {
		request.TLS = &tls.ConnectionState{
			Version:                     uint16(req.Request.Tls.Version),
			HandshakeComplete:           req.Request.Tls.HandshakeComplete,
			DidResume:                   req.Request.Tls.DidResume,
			CipherSuite:                 uint16(req.Request.Tls.CipherSuite),
			NegotiatedProtocol:          req.Request.Tls.NegotiatedProtocol,
			NegotiatedProtocolIsMutual:  true, // always true per https://pkg.go.dev/crypto/tls#ConnectionState
			ServerName:                  req.Request.Tls.ServerName,
			PeerCertificates:            make([]*x509.Certificate, len(req.Request.Tls.PeerCertificates.Cert)),
			VerifiedChains:              make([][]*x509.Certificate, len(req.Request.Tls.VerifiedChains)),
			SignedCertificateTimestamps: req.Request.Tls.SignedCertificateTimestamps,
			OCSPResponse:                req.Request.Tls.OcspResponse,
		}
		for i, certBytes := range req.Request.Tls.PeerCertificates.Cert {
			cert, err := x509.ParseCertificate(certBytes)
			if err != nil {
				return nil, err
			}
			request.TLS.PeerCertificates[i] = cert
		}
		for i, chain := range req.Request.Tls.VerifiedChains {
			request.TLS.VerifiedChains[i] = make([]*x509.Certificate, len(chain.Cert))
			for j, certBytes := range chain.Cert {
				cert, err := x509.ParseCertificate(certBytes)
				if err != nil {
					return nil, err
				}
				request.TLS.VerifiedChains[i][j] = cert
			}
		}
	}

	s.handler.ServeHTTP(writer, request)

	if err := clientConn.Close(); err != nil {
		return nil, fmt.Errorf("failed to close client conn: %w", err)
	}

	return &httppb.HTTPResponse{
		Header: grpcutils.GetHTTPHeader(writerHeaders),
	}, nil
}

// HandleSimple handles http requests over http2 using a simple request response model.
// Websockets are not supported.
func (s *Server) HandleSimple(ctx context.Context, r *httppb.HandleSimpleHTTPRequest) (*httppb.HandleSimpleHTTPResponse, error) {
	req, err := http.NewRequestWithContext(ctx, r.Method, r.Url, bytes.NewBuffer(r.Body))
	if err != nil {
		return nil, err
	}

	grpcutils.SetHeaders(req.Header, r.RequestHeaders)
	req.RequestURI = r.Url
	req.ContentLength = int64(len(r.Body))

	w := newResponseWriter()
	grpcutils.SetHeaders(w.Header(), r.ResponseHeaders)
	s.handler.ServeHTTP(w, req)

	resp := &httppb.HandleSimpleHTTPResponse{
		Code:    int32(w.statusCode),
		Headers: grpcutils.GetHTTPHeader(w.Header()),
		Body:    w.body.Bytes(),
	}

	if w.statusCode == http.StatusInternalServerError {
		return nil, grpcutils.GetGRPCErrorFromHTTPResponse(resp)
	}
	return resp, nil
}

type ResponseWriter struct {
	body       *bytes.Buffer
	header     http.Header
	statusCode int
}

// newResponseWriter returns very basic implementation of the http.ResponseWriter
func newResponseWriter() *ResponseWriter {
	return &ResponseWriter{
		body:       new(bytes.Buffer),
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (w *ResponseWriter) Header() http.Header {
	return w.header
}

func (w *ResponseWriter) Write(buf []byte) (int, error) {
	return w.body.Write(buf)
}

func (w *ResponseWriter) WriteHeader(code int) {
	w.statusCode = code
}

func (w *ResponseWriter) StatusCode() int {
	return w.statusCode
}

func (w *ResponseWriter) Body() *bytes.Buffer {
	return w.body
}
