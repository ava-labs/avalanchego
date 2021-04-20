// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

const (
	txDeduplicatorSize = 8192
)

type State interface {
	avax.UTXOState
	avax.StatusState
	avax.SingletonState
	TxState

	DeduplicateTx(tx *UniqueTx) *UniqueTx
}

type state struct {
	avax.UTXOState
	avax.StatusState
	avax.SingletonState
	TxState

	uniqueTxs cache.Deduplicator
}

func NewState(db database.Database, genesisCodec, codec codec.Manager) State {

}

// UniqueTx de-duplicates the transaction.
func (s *state) DeduplicateTx(tx *UniqueTx) *UniqueTx {
	return s.uniqueTxs.Deduplicate(tx).(*UniqueTx)
}
