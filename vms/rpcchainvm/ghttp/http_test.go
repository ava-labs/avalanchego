// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
	"github.com/ava-labs/avalanchego/proto/pb/test"
)

var (
	_ test.FooServer = (*fooServer)(nil)
	_ io.Reader      = (*infiniteStream)(nil)
)

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

func TestArbitrarilyLongResponse(t *testing.T) {
	require := require.New(t)

	listener := bufconn.Listen(1)
	server := grpc.NewServer()
	test.RegisterFooServer(server, fooServer{})

	go func() {
		require.NoError(server.Serve(listener))
	}()

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithInsecure())
	require.NoError(err)

	client := NewClient(httppb.NewHTTPClient(conn))

	w := &httptest.ResponseRecorder{}
	r := &http.Request{
		Header: map[string][]string{
			"Upgrade": {"foo"},
		},
		Body: io.NopCloser(infiniteStream{}),
	}

	client.ServeHTTP(w, r)
}

type fooServer struct {
	test.UnimplementedFooServer
}

func (fooServer) Foo(context.Context, *test.FooRequest) (*test.FooResponse, error) {
	return &test.FooResponse{}, nil
}

type infiniteStream struct{}

func (i infiniteStream) Read(p []byte) (n int, err error) {
	return len(p), nil
}
