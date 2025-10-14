// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/proto/pb/io/reader"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/greader"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gresponsewriter"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
	responsewriterpb "github.com/ava-labs/avalanchego/proto/pb/http/responsewriter"
)

var _ http.Handler = (*Client)(nil)

// Client is an http.Handler that talks over RPC.
type Client struct {
	client httppb.HTTPClient
	log    logging.Logger
}

// NewClient returns an HTTP handler database instance connected to a remote
// HTTP handler instance
func NewClient(client httppb.HTTPClient, log logging.Logger) *Client {
	return &Client{
		client: client,
		log:    log,
	}
}

func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// rfc2616#section-14.42: The Upgrade general-header allows the client
	// to specify a communication protocols it supports and would like to
	// use. Upgrade (e.g. websockets) is a more expensive transaction and
	// if not required use the less expensive HTTPSimple.
	//
	// Http/2 explicitly does not allow the use of the Upgrade header.
	// (ref: https://httpwg.org/specs/rfc9113.html#informational-responses)
	if !isUpgradeRequest(r) && !isHTTP2Request(r) {
		c.serveHTTPSimple(w, r)
		return
	}

	closer := grpcutils.ServerCloser{}
	defer closer.GracefulStop()

	// Wrap [w] with a lock to ensure that it is accessed in a thread-safe manner.
	w = gresponsewriter.NewLockedWriter(w)

	serverListener, err := grpcutils.NewListener()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	server := grpcutils.NewServer()
	closer.Add(server)
	responsewriterpb.RegisterWriterServer(server, gresponsewriter.NewServer(w))
	reader.RegisterReaderServer(server, greader.NewServer(r.Body))

	// Start responsewriter gRPC service.
	go grpcutils.Serve(serverListener, server)

	req := &httppb.HTTPRequest{
		ResponseWriter: &httppb.ResponseWriter{
			ServerAddr: serverListener.Addr().String(),
			Header:     make([]*httppb.Element, 0, len(r.Header)),
		},
		Request: &httppb.Request{
			Method:           r.Method,
			Proto:            r.Proto,
			ProtoMajor:       int32(r.ProtoMajor),
			ProtoMinor:       int32(r.ProtoMinor),
			Header:           make([]*httppb.Element, 0, len(r.Header)),
			ContentLength:    r.ContentLength,
			TransferEncoding: r.TransferEncoding,
			Host:             r.Host,
			Form:             make([]*httppb.Element, 0, len(r.Form)),
			PostForm:         make([]*httppb.Element, 0, len(r.PostForm)),
			RemoteAddr:       r.RemoteAddr,
			RequestUri:       r.RequestURI,
		},
	}
	for key, values := range w.Header() {
		req.ResponseWriter.Header = append(req.ResponseWriter.Header, &httppb.Element{
			Key:    key,
			Values: values,
		})
	}
	for key, values := range r.Header {
		req.Request.Header = append(req.Request.Header, &httppb.Element{
			Key:    key,
			Values: values,
		})
	}
	for key, values := range r.Form {
		req.Request.Form = append(req.Request.Form, &httppb.Element{
			Key:    key,
			Values: values,
		})
	}
	for key, values := range r.PostForm {
		req.Request.PostForm = append(req.Request.PostForm, &httppb.Element{
			Key:    key,
			Values: values,
		})
	}

	if r.URL != nil {
		req.Request.Url = &httppb.URL{
			Scheme:     r.URL.Scheme,
			Opaque:     r.URL.Opaque,
			Host:       r.URL.Host,
			Path:       r.URL.Path,
			RawPath:    r.URL.RawPath,
			ForceQuery: r.URL.ForceQuery,
			RawQuery:   r.URL.RawQuery,
			Fragment:   r.URL.Fragment,
		}

		if r.URL.User != nil {
			pwd, set := r.URL.User.Password()
			req.Request.Url.User = &httppb.Userinfo{
				Username:    r.URL.User.Username(),
				Password:    pwd,
				PasswordSet: set,
			}
		}
	}

	if r.TLS != nil {
		req.Request.Tls = &httppb.ConnectionState{
			Version:            uint32(r.TLS.Version),
			HandshakeComplete:  r.TLS.HandshakeComplete,
			DidResume:          r.TLS.DidResume,
			CipherSuite:        uint32(r.TLS.CipherSuite),
			NegotiatedProtocol: r.TLS.NegotiatedProtocol,
			ServerName:         r.TLS.ServerName,
			PeerCertificates: &httppb.Certificates{
				Cert: make([][]byte, len(r.TLS.PeerCertificates)),
			},
			VerifiedChains:              make([]*httppb.Certificates, len(r.TLS.VerifiedChains)),
			SignedCertificateTimestamps: r.TLS.SignedCertificateTimestamps,
			OcspResponse:                r.TLS.OCSPResponse,
		}
		for i, cert := range r.TLS.PeerCertificates {
			req.Request.Tls.PeerCertificates.Cert[i] = cert.Raw
		}
		for i, chain := range r.TLS.VerifiedChains {
			req.Request.Tls.VerifiedChains[i] = &httppb.Certificates{
				Cert: make([][]byte, len(chain)),
			}
			for j, cert := range chain {
				req.Request.Tls.VerifiedChains[i].Cert[j] = cert.Raw
			}
		}
	}

	reply, err := c.client.Handle(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	grpcutils.SetHeaders(w.Header(), reply.Header)
}

// serveHTTPSimple converts an http request to a gRPC HTTPRequest and returns the
// response to the client. Protocol upgrade requests (websockets) are not supported
// and should use ServeHTTP.
func (c *Client) serveHTTPSimple(w http.ResponseWriter, r *http.Request) {
	req, err := getHTTPSimpleRequest(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := c.client.HandleSimple(r.Context(), req)
	if err != nil {
		// Some errors will actually contain a valid resp, just need to unpack it
		var ok bool
		resp, ok = grpcutils.GetHTTPResponseFromError(err)
		if !ok {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := convertWriteResponse(w, resp); err != nil {
		c.log.Debug("failed sending HTTP response",
			zap.Error(err),
		)
	}
}

// getHTTPSimpleRequest takes an http request as input and returns a gRPC HandleSimpleHTTPRequest.
func getHTTPSimpleRequest(w http.ResponseWriter, r *http.Request) (*httppb.HandleSimpleHTTPRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return &httppb.HandleSimpleHTTPRequest{
		Method:          r.Method,
		Url:             r.RequestURI,
		Body:            body,
		RequestHeaders:  grpcutils.GetHTTPHeader(r.Header),
		ResponseHeaders: grpcutils.GetHTTPHeader(w.Header()),
	}, nil
}

// convertWriteResponse converts a gRPC HandleSimpleHTTPResponse to an HTTP response.
func convertWriteResponse(w http.ResponseWriter, resp *httppb.HandleSimpleHTTPResponse) error {
	grpcutils.SetHeaders(w.Header(), resp.Headers)
	w.WriteHeader(grpcutils.EnsureValidResponseCode(int(resp.Code)))
	_, err := w.Write(resp.Body)
	return err
}

// isUpgradeRequest returns true if the upgrade key exists in header and value is non empty.
func isUpgradeRequest(req *http.Request) bool {
	return req.Header.Get("Upgrade") != ""
}

func isHTTP2Request(req *http.Request) bool {
	return req.ProtoMajor == 2
}
