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

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// FakeResponder is a programmable [handlers.Responder] fake. It
// returns Resp/Err and records the last request in GotReq.
type FakeResponder[Req, Resp handlers.ProtoMessage] struct {
	Resp   Resp
	Err    error
	GotReq Req
}

// FakeLeafResponder is the [FakeResponder] bound to the leaf-range RPC.
type FakeLeafResponder = FakeResponder[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]

// FakeCodeResponder is the [FakeResponder] bound to the code-by-hash RPC.
type FakeCodeResponder = FakeResponder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]

// FakeBlockResponder is the [FakeResponder] bound to the block-batch RPC.
type FakeBlockResponder = FakeResponder[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse]

// Respond implements [handlers.Responder].
func (s *FakeResponder[Req, Resp]) Respond(_ context.Context, _ ids.NodeID, req Req) (Resp, error) {
	s.GotReq = req
	return s.Resp, s.Err
}

// MustMarshal calls [proto.Marshal] on m and fails the test on error.
func MustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	require.NoError(t, err)
	return b
}
