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
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

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

func (c *testConn) writeJSON(_ context.Context, msg interface{}, _ bool) error {
	return c.enc.Encode(msg)
}

func (c *testConn) writeJSONSkipDeadline(_ context.Context, msg interface{}, _, _ bool) error {
	return c.enc.Encode(msg)
}

func (*testConn) closed() <-chan interface{} { return nil }

func (*testConn) remoteAddr() string { return "" }

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
