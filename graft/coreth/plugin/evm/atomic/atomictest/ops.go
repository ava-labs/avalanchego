// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomictest

import (
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

func ConvertToAtomicOps(tx *atomic.Tx) (map[ids.ID]*avalancheatomic.Requests, error) {
	id, reqs, err := tx.AtomicOps()
	if err != nil {
		return nil, err
	}
	return map[ids.ID]*avalancheatomic.Requests{id: reqs}, nil
}
