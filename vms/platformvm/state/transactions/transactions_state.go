// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
)

// Mutable interface collects all methods updating
// transactions state upon blocks execution.
type Mutable interface {
	ValidatorState
	UTXOState

	GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error)
	AddRewardUTXO(txID ids.ID, utxo *avax.UTXO)
	GetSubnets() ([]*signed.Tx, error)
	AddSubnet(createSubnetTx *signed.Tx)
	GetChains(subnetID ids.ID) ([]*signed.Tx, error)
	AddChain(createChainTx *signed.Tx)
	GetTx(txID ids.ID) (*signed.Tx, status.Status, error)
	AddTx(tx *signed.Tx, status status.Status)
}

// Content interface collects all methods to query and mutate
// all transactions related state. Note this Content is a superset
// of Mutable
type Content interface {
	Mutable
	uptime.State
	avax.UTXOReader

	AddCurrentStaker(tx *signed.Tx, potentialReward uint64)
	DeleteCurrentStaker(tx *signed.Tx)
	AddPendingStaker(tx *signed.Tx)
	DeletePendingStaker(tx *signed.Tx)
	GetValidatorWeightDiffs(height uint64, subnetID ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error)
	MaxStakeAmount(
		subnetID ids.ID,
		nodeID ids.NodeID,
		startTime time.Time,
		endTime time.Time,
	) (uint64, error)
}

// Management interface collects all methods used to initialize
// transaction db upon vm initialization, along with methods to
// persist updated state.
type Management interface {
	SyncGenesis(
		genesisUtxos []*avax.UTXO,
		genesisValidator []*signed.Tx,
		genesisChains []*signed.Tx,
	) error
	LoadTxs() error

	WriteTxs() error
	CloseTxs() error
}

type TxState interface {
	Content
	Management
}
