// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"context"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/proto/pb/test"

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

type infiniteStream struct{}

func (infiniteStream) Read(p []byte) (n int, err error) {
	return len(p), nil
}
