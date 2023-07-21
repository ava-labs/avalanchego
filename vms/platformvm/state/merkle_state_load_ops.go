// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func (ms *merkleState) sync(genesis []byte) error {
	shouldInit, err := ms.shouldInit()
	if err != nil {
		return fmt.Errorf(
			"failed to check if the database is initialized: %w",
			err,
		)
	}

	// If the database is empty, create the platform chain anew using the
	// provided genesis state
	if shouldInit {
		if err := ms.init(genesis); err != nil {
			return fmt.Errorf(
				"failed to initialize the database: %w",
				err,
			)
		}
	}

	return nil /*ms.load()*/
}

func (ms *merkleState) shouldInit() (bool, error) {
	has, err := ms.singletonDB.Has(initializedKey)
	return !has, err
}

func (ms *merkleState) doneInit() error {
	return ms.singletonDB.Put(initializedKey, nil)
}

func (ms *merkleState) init(genesisBytes []byte) error {
	// Create the genesis block and save it as being accepted (We don't do
	// genesisBlock.Accept() because then it'd look for genesisBlock's
	// non-existent parent)
	genesisID := hashing.ComputeHash256Array(genesisBytes)
	genesisBlock, err := blocks.NewApricotCommitBlock(genesisID, 0 /*height*/)
	if err != nil {
		return err
	}

	genesisState, err := genesis.ParseState(genesisBytes)
	if err != nil {
		return err
	}
	if err := ms.syncGenesis(genesisBlock, genesisState); err != nil {
		return err
	}

	if err := ms.doneInit(); err != nil {
		return err
	}

	return ms.Commit()
}

func (ms *merkleState) syncGenesis(genesisBlk blocks.Block, genesis *genesis.State) error {
	genesisBlkID := genesisBlk.ID()
	ms.SetLastAccepted(genesisBlkID)
	ms.SetTimestamp(time.Unix(int64(genesis.Timestamp), 0))
	ms.SetCurrentSupply(constants.PrimaryNetworkID, genesis.InitialSupply)
	ms.AddStatelessBlock(genesisBlk)

	// Persist UTXOs that exist at genesis
	for _, utxo := range genesis.UTXOs {
		ms.AddUTXO(utxo)
	}

	// Persist primary network validator set at genesis
	for _, vdrTx := range genesis.Validators {
		tx, ok := vdrTx.Unsigned.(*txs.AddValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.AddValidatorTx but got %T", vdrTx.Unsigned)
		}

		stakeAmount := tx.Validator.Wght
		stakeDuration := tx.Validator.Duration()
		currentSupply, err := ms.GetCurrentSupply(constants.PrimaryNetworkID)
		if err != nil {
			return err
		}

		potentialReward := ms.rewards.Calculate(
			stakeDuration,
			stakeAmount,
			currentSupply,
		)
		newCurrentSupply, err := math.Add64(currentSupply, potentialReward)
		if err != nil {
			return err
		}

		staker, err := NewCurrentStaker(vdrTx.ID(), tx, potentialReward)
		if err != nil {
			return err
		}

		ms.PutCurrentValidator(staker)
		ms.AddTx(vdrTx, status.Committed)
		ms.SetCurrentSupply(constants.PrimaryNetworkID, newCurrentSupply)
	}

	for _, chain := range genesis.Chains {
		unsignedChain, ok := chain.Unsigned.(*txs.CreateChainTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chain.Unsigned)
		}

		// Ensure all chains that the genesis bytes say to create have the right
		// network ID
		if unsignedChain.NetworkID != ms.ctx.NetworkID {
			return avax.ErrWrongNetworkID
		}

		ms.AddChain(chain)
		ms.AddTx(chain, status.Committed)
	}

	// updateValidators is set to false here to maintain the invariant that the
	// primary network's validator set is empty before the validator sets are
	// initialized.
	return ms.write(false /*=updateValidators*/, 0)
}
