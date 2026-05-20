// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// FakeResponder records the last request in GotReq.
type FakeResponder[Req, Resp proto.Message] struct {
	Resp   Resp
	Err    *common.AppError
	GotReq Req
}

// FakeBlockResponder is the [FakeResponder] bound to the block-batch RPC.
type FakeBlockResponder = FakeResponder[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse]

func (f *FakeResponder[Req, Resp]) Respond(_ context.Context, _ ids.NodeID, req Req) (Resp, *common.AppError) {
	f.GotReq = req
	return f.Resp, f.Err
}

// MustMarshal marshals m deterministically and fails the test on error.
func MustMarshal(tb testing.TB, m proto.Message) []byte {
	tb.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	require.NoError(tb, err)
	return b
}
