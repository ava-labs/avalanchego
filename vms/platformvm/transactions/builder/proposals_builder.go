// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type ProposalsTxBuilder interface {
	NewAddValidatorTx(
		stakeAmt, // Amount the validator stakes
		startTime, // Unix time they start validating
		endTime uint64, // Unix time they stop validating
		nodeID ids.NodeID, // ID of the node we want to validate with
		rewardAddress ids.ShortID, // Address to send reward to, if applicable
		shares uint32, // 10,000 times percentage of reward taken from delegators
		keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens
		changeAddr ids.ShortID, // Address to send change to, if there is any
	) (*signed.Tx, error)

	NewAddDelegatorTx(
		stakeAmt, // Amount the delegator stakes
		startTime, // Unix time they start delegating
		endTime uint64, // Unix time they stop delegating
		nodeID ids.NodeID, // ID of the node we are delegating to
		rewardAddress ids.ShortID, // Address to send reward to, if applicable
		keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens
		changeAddr ids.ShortID, // Address to send change to, if there is any
	) (*signed.Tx, error)

	NewAddSubnetValidatorTx(
		weight, // Sampling weight of the new validator
		startTime, // Unix time they start delegating
		endTime uint64, // Unix time they top delegating
		nodeID ids.NodeID, // ID of the node validating
		subnetID ids.ID, // ID of the subnet the validator will validate
		keys []*crypto.PrivateKeySECP256K1R, // Keys to use for adding the validator
		changeAddr ids.ShortID, // Address to send change to, if there is any
	) (*signed.Tx, error)

	NewAdvanceTimeTx(timestamp time.Time) (*signed.Tx, error)
	NewRewardValidatorTx(txID ids.ID) (*signed.Tx, error)
}

func (b *builder) NewAddValidatorTx(
	stakeAmt, // Amount the validator stakes
	startTime, // Unix time they start validating
	endTime uint64, // Unix time they stop validating
	nodeID ids.NodeID, // ID of the node we want to validate with
	rewardAddress ids.ShortID, // Address to send reward to, if applicable
	shares uint32, // 10,000 times percentage of reward taken from delegators
	keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	ins, unlockedOuts, lockedOuts, signers, err := b.Stake(keys, stakeAmt, b.cfg.AddStakerTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// Create the tx
	utx := &unsigned.AddValidatorTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         unlockedOuts,
		}},
		Validator: validators.Validator{
			NodeID: nodeID,
			Start:  startTime,
			End:    endTime,
			Wght:   stakeAmt,
		},
		Stake: lockedOuts,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		Shares: shares,
	}
	tx := &signed.Tx{Unsigned: utx}
	if err := tx.Sign(unsigned.Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddDelegatorTx(
	stakeAmt, // Amount the delegator stakes
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they stop delegating
	nodeID ids.NodeID, // ID of the node we are delegating to
	rewardAddress ids.ShortID, // Address to send reward to, if applicable
	keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	ins, unlockedOuts, lockedOuts, signers, err := b.Stake(keys, stakeAmt, b.cfg.AddStakerTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// Create the tx
	utx := &unsigned.AddDelegatorTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         unlockedOuts,
		}},
		Validator: validators.Validator{
			NodeID: nodeID,
			Start:  startTime,
			End:    endTime,
			Wght:   stakeAmt,
		},
		Stake: lockedOuts,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
	}
	tx := &signed.Tx{Unsigned: utx}
	if err := tx.Sign(unsigned.Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddSubnetValidatorTx(
	weight, // Sampling weight of the new validator
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they top delegating
	nodeID ids.NodeID, // ID of the node validating
	subnetID ids.ID, // ID of the subnet the validator will validate
	keys []*crypto.PrivateKeySECP256K1R, // Keys to use for adding the validator
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	ins, outs, _, signers, err := b.Stake(keys, 0, b.cfg.TxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	subnetAuth, subnetSigners, err := b.Authorize(b.state, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	signers = append(signers, subnetSigners)

	// Create the tx
	utx := &unsigned.AddSubnetValidatorTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Validator: validators.SubnetValidator{
			Validator: validators.Validator{
				NodeID: nodeID,
				Start:  startTime,
				End:    endTime,
				Wght:   weight,
			},
			Subnet: subnetID,
		},
		SubnetAuth: subnetAuth,
	}
	tx := &signed.Tx{Unsigned: utx}
	if err := tx.Sign(unsigned.Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.SyntacticVerify(b.ctx)
}

// newAdvanceTimeTx creates a new tx that, if it is accepted and followed by a
// Commit block, will set the chain's timestamp to [timestamp].
func (b *builder) NewAdvanceTimeTx(timestamp time.Time) (*signed.Tx, error) {
	utx := &unsigned.AdvanceTimeTx{Time: uint64(timestamp.Unix())}
	tx := &signed.Tx{Unsigned: utx}
	if err := tx.Sign(unsigned.Codec, nil); err != nil {
		return nil, err
	}
	return tx, utx.SyntacticVerify(b.ctx)
}

// RewardStakerTx creates a new transaction that proposes to remove the staker
// [validatorID] from the default validator set.
func (b *builder) NewRewardValidatorTx(txID ids.ID) (*signed.Tx, error) {
	utx := &unsigned.RewardValidatorTx{TxID: txID}
	tx := &signed.Tx{Unsigned: utx}
	if err := tx.Sign(unsigned.Codec, nil); err != nil {
		return nil, err
	}
	return tx, utx.SyntacticVerify(b.ctx)
}
