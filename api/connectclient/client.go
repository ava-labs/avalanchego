// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connectclient

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"

	"github.com/ava-labs/avalanchego/api/server"
)

var _ connect.Interceptor = (*SetRouteHeaderInterceptor)(nil)

// SetRouteHeaderInterceptor sets the api routing header for connect-rpc
// requests
type SetRouteHeaderInterceptor struct {
	Route []string
}

func (s SetRouteHeaderInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		for _, route := range s.Route {
			request.Header().Add(server.HTTPHeaderRoute, route)
		}
		return next(ctx, request)
	}
}

func (s SetRouteHeaderInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		for _, route := range s.Route {
			conn.RequestHeader().Add(server.HTTPHeaderRoute, route)
		}
		return conn
	}
}

func (SetRouteHeaderInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func New() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr) // Skip TLS to use h2c
			},
		},
	}
}
