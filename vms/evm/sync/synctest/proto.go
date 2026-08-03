// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
)

// FakeResponder is a programmable [handlers.Responder] that records the last
// request in GotReq.
type FakeResponder[Req, Resp handlers.ProtoMessage] struct {
	Resp   Resp
	Err    error
	GotReq Req
}

func (f *FakeResponder[Req, Resp]) Respond(_ context.Context, _ ids.NodeID, req Req) (Resp, error) {
	f.GotReq = req
	return f.Resp, f.Err
}

// MustMarshal marshals m deterministically, so the bytes are stable across
// calls, and fails the test on error.
func MustMarshal(tb testing.TB, m proto.Message) []byte {
	tb.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	require.NoError(tb, err)
	return b
}
