// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/x/sync"
)

// NewGetRangeProofHandler returns a handler that services GetRangeProof requests
// using the provided Firewood database for p2p connections.
func NewGetRangeProofHandler(db *ffi.Database) *sync.GetRangeProofHandler[*RangeProof, struct{}] {
	return sync.NewGetRangeProofHandler(&database{db: db}, rangeProofMarshaler{})
}

// NewGetChangeProofHandler returns a handler that services GetChangeProof requests
// using the provided Firewood database for p2p connections.
func NewGetChangeProofHandler(db *ffi.Database) *sync.GetChangeProofHandler[*RangeProof, struct{}] {
	return sync.NewGetChangeProofHandler(&database{db: db}, rangeProofMarshaler{}, changeProofMarshaler{})
}
