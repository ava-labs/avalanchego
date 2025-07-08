// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
)

var _ io.Reader = (*infiniteStream)(nil)

func TestConvertWriteResponse(t *testing.T) {
	scenerios := map[string]struct {
		resp *httppb.HandleSimpleHTTPResponse
	}{
		"empty response": {
			resp: &httppb.HandleSimpleHTTPResponse{},
		},
		"response header value empty": {
			resp: &httppb.HandleSimpleHTTPResponse{
				Code: 500,
				Body: []byte("foo"),
				Headers: []*httppb.Element{
					{
						Key: "foo",
					},
				},
			},
		},
		"response header key empty": {
			resp: &httppb.HandleSimpleHTTPResponse{
				Code: 200,
				Body: []byte("foo"),
				Headers: []*httppb.Element{
					{
						Values: []string{"foo"},
					},
				},
			},
		},
	}
	for testName, scenerio := range scenerios {
		t.Run(testName, func(t *testing.T) {
			w := httptest.NewRecorder()
			require.NoError(t, convertWriteResponse(w, scenerio.resp))
		})
	}
}

func TestRequestClientArbitrarilyLongBody(t *testing.T) {
	require := require.New(t)

	listener := bufconn.Listen(0)
	server := grpc.NewServer()
	httppb.RegisterHTTPServer(server, &httppb.UnimplementedHTTPServer{})

	go func() {
		_ = server.Serve(listener)
	}()

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(err)

	client := NewClient(httppb.NewHTTPClient(conn))

	w := &httptest.ResponseRecorder{}
	r := &http.Request{
		Header: map[string][]string{
			"Upgrade": {"foo"}, // Make this look like a streaming request
		},
		Body: io.NopCloser(infiniteStream{}),
	}

	client.ServeHTTP(w, r) // Shouldn't block forever reading the body
}

// Tests that writes to the http response in the server are propagated to the
// client
func TestHttpResponse(t *testing.T) {
	tests := []struct {
		name            string
		requestHeaders  http.Header
		responseHeaders http.Header
	}{
		{
			// Requests with an upgrade header do not use the "Simple*" http response
			// apis and must be separately tested
			name: "upgrade request header specified",
			requestHeaders: http.Header{
				"Upgrade": {"upgrade"},
				"foo":     {"foo"},
			},
			responseHeaders: http.Header{},
		},
		{
			name: "arbitrary request headers",
			requestHeaders: http.Header{
				"foo": {"foo"},
			},
			responseHeaders: http.Header{},
		},
		{
			name:           "response header set",
			requestHeaders: http.Header{},
			responseHeaders: http.Header{
				"foo": {"foo"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			wantHandlerHeaders := http.Header{}
			wantHandlerHeaders.Add("Bar", "bar")
			wantHandlerHeaders.Add("Content-Type", "application/json")
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range wantHandlerHeaders {
					w.Header().Set(k, v[0])
				}

				_, _ = w.Write([]byte("baz"))
			})

			listener, err := grpcutils.NewListener()
			require.NoError(err)
			server := grpc.NewServer()
			httppb.RegisterHTTPServer(server, NewServer(handler))

			go func() {
				_ = server.Serve(listener)
			}()

			conn, err := grpc.NewClient(
				listener.Addr().String(),
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			require.NoError(err)

			recorder := &httptest.ResponseRecorder{
				HeaderMap: maps.Clone(tt.responseHeaders),
				Body:      bytes.NewBuffer(nil),
			}

			client := NewClient(httppb.NewHTTPClient(conn))
			request := &http.Request{
				Body:   io.NopCloser(strings.NewReader("foo")),
				Header: tt.requestHeaders,
			}

			client.ServeHTTP(recorder, request)

			wantResponseHeaders := maps.Clone(tt.responseHeaders)
			for k, v := range wantHandlerHeaders {
				wantResponseHeaders.Add(k, v[0])
			}

			require.Equal(wantResponseHeaders, recorder.Header())
			require.Equal(http.StatusOK, recorder.Code)
			require.Equal("baz", recorder.Body.String())

			require.Equal(tt.requestHeaders, request.Header)
		})
	}
}

type infiniteStream struct{}

func (infiniteStream) Read(p []byte) (n int, err error) {
	return len(p), nil
}
