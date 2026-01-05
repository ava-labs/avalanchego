// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/x/sync"
)

func NewGetRangeProofHandler(db *ffi.Database) *sync.GetRangeProofHandler[*RangeProof, struct{}] {
	return sync.NewGetRangeProofHandler(&database{db}, rangeProofMarshaler{})
}

func NewGetChangeProofHandler(db *ffi.Database) *sync.GetChangeProofHandler[*RangeProof, struct{}] {
	return sync.NewGetChangeProofHandler(&database{db}, rangeProofMarshaler{}, changeProofMarshaler{})
}
