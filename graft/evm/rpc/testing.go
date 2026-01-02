// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"encoding/json"
	"io"
)

// testConn is a test implementation of the serverConn interface.
type testConn struct {
	enc *json.Encoder
}

func (c *testConn) writeJSON(ctx context.Context, msg interface{}, isError bool) error {
	return c.enc.Encode(msg)
}

func (c *testConn) writeJSONSkipDeadline(ctx context.Context, msg interface{}, isError bool, skip bool) error {
	return c.enc.Encode(msg)
}

func (c *testConn) closed() <-chan interface{} { return nil }

func (c *testConn) remoteAddr() string { return "" }

// NewTestNotifier creates a Notifier for testing that writes to the given writer.
// This is exported so that tests in package rpc_test can create Notifiers without
// accessing internal types.
func NewTestNotifier(w io.Writer, subID ID) *Notifier {
	return &Notifier{
		h: &handler{
			conn: &testConn{enc: json.NewEncoder(w)},
		},
		sub:       &Subscription{ID: subID},
		activated: true,
	}
}
