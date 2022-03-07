// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"io"
	"net/http"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/api/proto/ghttpproto"
	"github.com/ava-labs/avalanchego/api/proto/gresponsewriterproto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gresponsewriter"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
)

var _ http.Handler = &Client{}

// Client is an http.Handler that talks over RPC.
type Client struct {
	client ghttpproto.HTTPClient
	broker *plugin.GRPCBroker
}

// NewClient returns an HTTP handler database instance connected to a remote
// HTTP handler instance
func NewClient(client ghttpproto.HTTPClient, broker *plugin.GRPCBroker) *Client {
	return &Client{
		client: client,
		broker: broker,
	}
}

func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// rfc2616#section-14.42: The Upgrade general-header allows the client
	// to specify a communication protocols it supports and would like to
	// use. Upgrade (e.g. websockets) is a more expensive transaction and
	// if not required use the less expensive HTTPSimple.
	if !isUpgradeRequest(r) {
		c.serveHTTPSimple(w, r)
		return
	}

	closer := grpcutils.ServerCloser{}
	defer closer.GracefulStop()

	// Wrap [w] with a lock to ensure that it is accessed in a thread-safe manner.
	w = gresponsewriter.NewLockedWriter(w)

	readerWriterID := c.broker.NextId()
	// Start responsewriter gRPC service.
	go c.broker.AcceptAndServe(readerWriterID, func(opts []grpc.ServerOption) *grpc.Server {
		opts = append(opts,
			grpc.MaxRecvMsgSize(math.MaxInt),
			grpc.MaxSendMsgSize(math.MaxInt),
		)
		server := grpc.NewServer(opts...)
		closer.Add(server)
		gresponsewriterproto.RegisterWriterServer(server, gresponsewriter.NewServer(w, c.broker))
		return server
	})

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}

	req := &ghttpproto.HTTPRequest{
		ResponseWriter: &ghttpproto.ResponseWriter{
			Id:     readerWriterID,
			Header: make([]*ghttpproto.Element, 0, len(r.Header)),
		},
		Request: &ghttpproto.Request{
			Method:           r.Method,
			Proto:            r.Proto,
			ProtoMajor:       int32(r.ProtoMajor),
			ProtoMinor:       int32(r.ProtoMinor),
			Header:           make([]*ghttpproto.Element, 0, len(r.Header)),
			Body:             body,
			ContentLength:    r.ContentLength,
			TransferEncoding: r.TransferEncoding,
			Host:             r.Host,
			Form:             make([]*ghttpproto.Element, 0, len(r.Form)),
			PostForm:         make([]*ghttpproto.Element, 0, len(r.PostForm)),
			RemoteAddr:       r.RemoteAddr,
			RequestUri:       r.RequestURI,
		},
	}
	for key, values := range w.Header() {
		req.ResponseWriter.Header = append(req.ResponseWriter.Header, &ghttpproto.Element{
			Key:    key,
			Values: values,
		})
	}
	for key, values := range r.Header {
		req.Request.Header = append(req.Request.Header, &ghttpproto.Element{
			Key:    key,
			Values: values,
		})
	}
	for key, values := range r.Form {
		req.Request.Form = append(req.Request.Form, &ghttpproto.Element{
			Key:    key,
			Values: values,
		})
	}
	for key, values := range r.PostForm {
		req.Request.PostForm = append(req.Request.PostForm, &ghttpproto.Element{
			Key:    key,
			Values: values,
		})
	}

	if r.URL != nil {
		req.Request.Url = &ghttpproto.URL{
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
			req.Request.Url.User = &ghttpproto.Userinfo{
				Username:    r.URL.User.Username(),
				Password:    pwd,
				PasswordSet: set,
			}
		}
	}

	if r.TLS != nil {
		req.Request.Tls = &ghttpproto.ConnectionState{
			Version:            uint32(r.TLS.Version),
			HandshakeComplete:  r.TLS.HandshakeComplete,
			DidResume:          r.TLS.DidResume,
			CipherSuite:        uint32(r.TLS.CipherSuite),
			NegotiatedProtocol: r.TLS.NegotiatedProtocol,
			ServerName:         r.TLS.ServerName,
			PeerCertificates: &ghttpproto.Certificates{
				Cert: make([][]byte, len(r.TLS.PeerCertificates)),
			},
			VerifiedChains:              make([]*ghttpproto.Certificates, len(r.TLS.VerifiedChains)),
			SignedCertificateTimestamps: r.TLS.SignedCertificateTimestamps,
			OcspResponse:                r.TLS.OCSPResponse,
		}
		for i, cert := range r.TLS.PeerCertificates {
			req.Request.Tls.PeerCertificates.Cert[i] = cert.Raw
		}
		for i, chain := range r.TLS.VerifiedChains {
			req.Request.Tls.VerifiedChains[i] = &ghttpproto.Certificates{
				Cert: make([][]byte, len(chain)),
			}
			for j, cert := range chain {
				req.Request.Tls.VerifiedChains[i].Cert[j] = cert.Raw
			}
		}
	}

	_, err = c.client.Handle(r.Context(), req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// serveHTTPSimple converts an http request to a gRPC HTTPRequest and returns the
// response to the client. Protocol upgrade requests (websockets) are not supported
// and should use ServeHTTP. Based on https://www.weave.works/blog/turtles-way-http-grpc.
func (c *Client) serveHTTPSimple(w http.ResponseWriter, r *http.Request) {
	req, err := getHTTPRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// getHTTPRequest takes an http request as input and returns a gRPC HandleSimpleHTTPRequest.
func getHTTPRequest(r *http.Request) (*ghttpproto.HandleSimpleHTTPRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return &ghttpproto.HandleSimpleHTTPRequest{
		Method:  r.Method,
		Url:     r.RequestURI,
		Body:    body,
		Headers: grpcutils.GetHTTPHeader(r.Header),
	}, nil
}

// convertWriteResponse converts a gRPC HandleSimpleHTTPResponse to an HTTP response.
func convertWriteResponse(w http.ResponseWriter, resp *ghttpproto.HandleSimpleHTTPResponse) error {
	grpcutils.MergeHTTPHeader(resp.Headers, w.Header())
	w.WriteHeader(int(resp.Code))
	_, err := w.Write(resp.Body)
	return err
}

// isUpgradeRequest returns true if the upgrade key exists in header and is non empty.
func isUpgradeRequest(req *http.Request) bool {
	if len(req.Header) == 0 {
		return false
	}
	return req.Header.Get("Upgrade") != ""
}
