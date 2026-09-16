// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database/merkle/sync"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// NewGetProofHandler returns a handler that services proof requests
// using the provided Firewood database for p2p connections. Requests it
// cannot serve are logged on log; see [sync.NewProofHandler].
func NewGetProofHandler(log logging.Logger, db *ffi.Database, registerer prometheus.Registerer) (*sync.ProofHandler[*RangeProof, struct{}], error) {
	return sync.NewProofHandler(log, &database{db: db}, rangeProofMarshaler{}, changeProofMarshaler{}, registerer)
}
