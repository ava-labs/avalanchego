// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncertest

import (
	"testing"

	"github.com/ava-labs/avalanchego/firewood/syncer"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/x/sync"
	"github.com/ava-labs/firewood-go-ethhash/ffi"
)

func NewTestRangeProofHandler(t *testing.T, db *ffi.Database) *p2p.Client {
	return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, sync.NewGetRangeProofHandler(syncer.NewDatabase(db), syncer.RangeProofMarshaler{}))
}
func NewTestChangeProofHandler(t *testing.T, db *ffi.Database) *p2p.Client {
	return p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, sync.NewGetChangeProofHandler(syncer.NewDatabase(db), syncer.RangeProofMarshaler{}, syncer.ChangeProofMarshaler{}))
}
