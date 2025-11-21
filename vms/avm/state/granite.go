// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import "github.com/ava-labs/avalanchego/ids"

var _ ChainDB = (*NoChainDB)(nil)

type NoChainDB struct{}

func (*NoChainDB) Close() error {
	return nil
}

func (*NoChainDB) Abort() {}

// AddAtomicTx is a no-op because atomic txs are not a part of chain state
func (*NoChainDB) AddAtomicTx(ids.ID) {}

// Repair is a no-op because the db is always written atomically with the
// rest of the State.
func (*NoChainDB) Repair(VM, State) error {
	return nil
}
