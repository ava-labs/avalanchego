// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********

package rpc_test

import (
	"context"
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/graft/coreth/rpc"
)

// This example configures a HTTP-based RPC client with two options - one setting the
// overall request timeout, the other adding a custom HTTP header to all requests.
func ExampleDialOptions() {
	tokenHeader := rpc.WithHeader("x-token", "foo")
	httpClient := rpc.WithHTTPClient(&http.Client{
		Timeout: 10 * time.Second,
	})

	ctx := context.Background()
	c, err := rpc.DialOptions(ctx, "http://rpc.example.com", httpClient, tokenHeader)
	if err != nil {
		panic(err)
	}
	c.Close()
}
