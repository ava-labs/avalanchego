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
		require.NoError(server.Serve(listener))
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
	require := require.New(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(123)
		w.Header()["Foo"] = []string{"bar"}
		_, err := w.Write([]byte{1, 2, 3})
		require.NoError(err)
	})

	listener, err := grpcutils.NewListener()
	require.NoError(err)
	server := grpc.NewServer()
	httppb.RegisterHTTPServer(server, NewServer(handler))

	go func() {
		require.NoError(server.Serve(listener))
	}()

	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)

	client := NewClient(httppb.NewHTTPClient(conn))

	recorder := &httptest.ResponseRecorder{
		Body: bytes.NewBuffer(nil),
	}

	request := &http.Request{
		Body: io.NopCloser(strings.NewReader("bar")),
		Header: http.Header{
			"Upgrade": {"upgrade"},
			"bar":     {"bar"},
		},
		Trailer: http.Header{
			"bar": {"bar"},
		},
		RemoteAddr: "bar",
	}

	client.ServeHTTP(recorder, request)

	require.Equal(123, recorder.Code)
	require.Equal([]string{"bar"}, recorder.Header()["Foo"])
	require.Equal([]byte{1, 2, 3}, recorder.Body.Bytes())
}

type infiniteStream struct{}

func (infiniteStream) Read(p []byte) (n int, err error) {
	return len(p), nil
}
