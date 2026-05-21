// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/database/merkle/sync"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// NewGetProofHandler returns a handler that services proof requests
// using the provided Firewood database for p2p connections.
func NewGetProofHandler(db *ffi.Database, log logging.Logger) *sync.ProofHandler[*RangeProof, struct{}] {
	return sync.NewProofHandler(&database{db: db}, log, rangeProofMarshaler{}, changeProofMarshaler{})
}
