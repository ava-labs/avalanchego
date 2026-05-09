// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/database/merkle/sync"
)

// NewGetProofHandler returns a handler that services proof requests
// using the provided Firewood database for p2p connections.
func NewGetProofHandler(db *ffi.Database) *sync.ProofHandler[*RangeProof, *ChangeProof] {
	return sync.NewProofHandler(&database{db: db}, rangeProofMarshaler{}, changeProofMarshaler{})
}
