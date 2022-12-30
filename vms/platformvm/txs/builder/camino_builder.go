// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/msig"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ CaminoBuilder = (*caminoBuilder)(nil)

	errKeyMissing       = errors.New("couldn't find key matching address")
	errWrongNodeKeyType = errors.New("node key type isn't *crypto.PrivateKeySECP256K1R")
)

type CaminoBuilder interface {
	Builder
	CaminoTxBuilder
	utxo.Spender
}

type CaminoTxBuilder interface {
	NewAddAddressStateTx(
		address ids.ShortID,
		remove bool,
		state uint8,
		keys []*crypto.PrivateKeySECP256K1R,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	NewDepositTx(
		amount uint64,
		duration uint32,
		depositOfferID ids.ID,
		rewardAddress ids.ShortID,
		keys []*crypto.PrivateKeySECP256K1R,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	NewUnlockDepositTx(
		lockTxIDs []ids.ID,
		keys []*crypto.PrivateKeySECP256K1R,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	NewRegisterNodeTx(
		OldNodeID ids.NodeID,
		NewNodeID ids.NodeID,
		ConsortiumMemberAddress ids.ShortID,
		keys []*crypto.PrivateKeySECP256K1R,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)
}

func NewCamino(
	ctx *snow.Context,
	cfg *config.Config,
	clk *mockable.Clock,
	fx fx.Fx,
	state state.Chain,
	atomicUTXOManager avax.AtomicUTXOManager,
	utxoSpender utxo.Spender,
) CaminoBuilder {
	return &caminoBuilder{
		builder: builder{
			AtomicUTXOManager: atomicUTXOManager,
			Spender:           utxoSpender,
			state:             state,
			cfg:               cfg,
			ctx:               ctx,
			clk:               clk,
			fx:                fx,
		},
	}
}

type caminoBuilder struct {
	builder
}

func (b *caminoBuilder) NewAddValidatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	shares uint32,
	keys []*crypto.PrivateKeySECP256K1R,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	caminoGenesis, err := b.builder.state.CaminoConfig()
	if err != nil {
		return nil, err
	}

	if !caminoGenesis.LockModeBondDeposit {
		return b.builder.NewAddValidatorTx(
			stakeAmount,
			startTime,
			endTime,
			nodeID,
			rewardAddress,
			shares,
			keys,
			changeAddr,
		)
	}

	ins, outs, signers, err := b.Lock(keys, stakeAmount, b.cfg.AddPrimaryNetworkValidatorFee, locked.StateBonded, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx := &txs.CaminoAddValidatorTx{
		AddValidatorTx: txs.AddValidatorTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    b.ctx.NetworkID,
				BlockchainID: b.ctx.ChainID,
				Ins:          ins,
				Outs:         outs,
			}},
			Validator: validator.Validator{
				NodeID: nodeID,
				Start:  startTime,
				End:    endTime,
				Wght:   stakeAmount,
			},
			RewardsOwner: &secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{rewardAddress},
			},
		},
	}

	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewAddSubnetValidatorTx(
	weight,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	subnetID ids.ID,
	keys []*crypto.PrivateKeySECP256K1R,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	tx, err := b.builder.NewAddSubnetValidatorTx(
		weight,
		startTime,
		endTime,
		nodeID,
		subnetID,
		keys,
		changeAddr,
	)
	if err != nil {
		return nil, err
	}

	if caminoGenesis, err := b.builder.state.CaminoConfig(); err != nil {
		return nil, err
	} else if !caminoGenesis.VerifyNodeSignature {
		return tx, nil
	}

	nodeSigners, err := getSigner(keys, ids.ShortID(nodeID))
	if err != nil {
		return nil, err
	}

	if err := tx.Sign(txs.Codec, [][]*crypto.PrivateKeySECP256K1R{nodeSigners}); err != nil {
		return nil, err
	}

	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewRewardValidatorTx(txID ids.ID) (*txs.Tx, error) {
	if state, err := b.builder.state.CaminoConfig(); err != nil {
		return nil, err
	} else if !state.LockModeBondDeposit {
		return b.builder.NewRewardValidatorTx(txID)
	}

	ins, outs, err := b.Unlock(b.state, []ids.ID{txID}, locked.StateBonded)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx := &txs.CaminoRewardValidatorTx{
		RewardValidatorTx: txs.RewardValidatorTx{TxID: txID},
		Ins:               ins,
		Outs:              outs,
	}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}

	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewAddAddressStateTx(
	address ids.ShortID,
	remove bool,
	state uint8,
	keys []*crypto.PrivateKeySECP256K1R,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	ins, outs, signers, err := b.Lock(keys, 0, b.cfg.TxFee, locked.StateUnlocked, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the tx
	utx := &txs.AddAddressStateTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Address: address,
		Remove:  remove,
		State:   state,
	}
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}

	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewDepositTx(
	amount uint64,
	duration uint32,
	depositOfferID ids.ID,
	rewardAddress ids.ShortID,
	keys []*crypto.PrivateKeySECP256K1R,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	ins, outs, signers, err := b.Lock(keys, amount, b.cfg.TxFee, locked.StateDeposited, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx := &txs.DepositTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		DepositOfferID:  depositOfferID,
		DepositDuration: duration,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
	}

	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewUnlockDepositTx(
	lockTxIDs []ids.ID,
	keys []*crypto.PrivateKeySECP256K1R,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	var ins []*avax.TransferableInput
	var outs []*avax.TransferableOutput
	var signers [][]*crypto.PrivateKeySECP256K1R

	// unlocking
	ins, outs, signers, err := b.UnlockDeposit(b.state, keys, lockTxIDs)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// burning fee
	feeIns, feeOuts, feeSigners, err := b.Lock(keys, 0, b.cfg.TxFee, locked.StateUnlocked, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	ins = append(ins, feeIns...)
	outs = append(outs, feeOuts...)
	signers = append(signers, feeSigners...)

	// we need to sort ins/outs/signers before using them in tx
	// UnlockDeposit returns unsorted results and we appended arrays
	avax.SortTransferableInputsWithSigners(ins, signers)
	avax.SortTransferableOutputs(outs, txs.Codec)

	utx := &txs.UnlockDepositTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
	}

	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewRegisterNodeTx(
	oldNodeID ids.NodeID,
	newNodeID ids.NodeID,
	consortiumMemberAddress ids.ShortID,
	keys []*crypto.PrivateKeySECP256K1R,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	ins, outs, signers, err := b.Lock(keys, 0, b.cfg.TxFee, locked.StateUnlocked, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	nodeSigners := []*crypto.PrivateKeySECP256K1R{}
	if newNodeID != ids.EmptyNodeID {
		nodeSigners, err = getSigner(keys, ids.ShortID(newNodeID))
		if err != nil {
			return nil, err
		}
	}
	signers = append(signers, nodeSigners)

	consortiumMemberOwner, err := msig.GetOwner(b.state, consortiumMemberAddress)
	if err != nil {
		return nil, err
	}

	consortiumSigners, err := getSigners(keys, consortiumMemberOwner.Addrs)
	if err != nil {
		return nil, err
	}
	signers = append(signers, consortiumSigners)

	kc := secp256k1fx.NewKeychain(consortiumSigners...)
	sigIndices, _, able := kc.Match(consortiumMemberOwner, b.clk.Unix())
	if !able {
		return nil, fmt.Errorf("failed to get consortium member auth: %w", err)
	}

	utx := &txs.RegisterNodeTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		OldNodeID:               oldNodeID,
		NewNodeID:               newNodeID,
		ConsortiumMemberAuth:    &secp256k1fx.Input{SigIndices: sigIndices},
		ConsortiumMemberAddress: consortiumMemberAddress,
	}

	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func getSigner(
	keys []*crypto.PrivateKeySECP256K1R,
	address ids.ShortID,
) ([]*crypto.PrivateKeySECP256K1R, error) {
	return getSigners(keys, []ids.ShortID{address})
}

func getSigners(
	keys []*crypto.PrivateKeySECP256K1R,
	addresses []ids.ShortID,
) ([]*crypto.PrivateKeySECP256K1R, error) {
	signers := make([]*crypto.PrivateKeySECP256K1R, len(addresses))
	for i, addr := range addresses {
		signer, found := secp256k1fx.NewKeychain(keys...).Get(addr)
		if !found {
			return nil, fmt.Errorf("%w %s", errKeyMissing, addr.String())
		}

		key, ok := signer.(*crypto.PrivateKeySECP256K1R)
		if !ok {
			return nil, errWrongNodeKeyType
		}

		signers[i] = key
	}
	return signers, nil
}
