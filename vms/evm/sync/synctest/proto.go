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

// FakeResponder is a programmable [handlers.Responder] fake. It
// returns Resp/Err and records the last request in GotReq.
type FakeResponder[Req, Resp handlers.ProtoMessage] struct {
	Resp   Resp
	Err    error
	GotReq Req
}

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
