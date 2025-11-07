// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

var _ client = (*testClient)(nil)

// TestEndpointRequesterLongResponse tests that [EndpointRequester.SendRequest]
// respects context cancellation when draining a long response
func TestEndpointRequesterLongResponse(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"foobar"}`))
			w.(http.Flusher).Flush()

			// Try to keep sending data even after the client has received the
			// response to try to block them while draining the http body.
			for {
				w.Write([]byte("foo"))
				w.(http.Flusher).Flush()
			}
		}),
	)

	gotResponse := make(chan struct{})
	client := avalancheEndpointRequester{
		client: testClient{
			client: httpClient{
				c: http.DefaultClient,
			},
			gotResponse: gotResponse,
		},
		uri: server.URL,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	eg := errgroup.Group{}
	eg.Go(func() error {
		return client.SendRequest(ctx, "foo", nil, new(any))
	})

	// Block after we receive the response to check that context cancellation is
	// respected while draining the response body.
	<-gotResponse
	cancel()

	err := eg.Wait()
	require.ErrorIs(t, err, context.Canceled)
}

type testClient struct {
	client
	gotResponse chan struct{}
}

func (t testClient) Send(req *http.Request) (*http.Response, error) {
	response, err := t.client.Send(req)
	close(t.gotResponse)

	return response, err
}
