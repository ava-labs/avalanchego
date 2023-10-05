// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ CaminoBuilder = (*caminoBuilder)(nil)

	fakeTreasuryKey      = secp256k1.FakePrivateKey(treasury.Addr)
	fakeTreasuryKeychain = secp256k1fx.NewKeychain(fakeTreasuryKey)

	errKeyMissing       = errors.New("couldn't find key matching address")
	errWrongNodeKeyType = errors.New("node key type isn't *secp256k1.PrivateKey")
	errNotSECPOwner     = errors.New("owner is not *secp256k1fx.OutputOwners")
	errWrongLockMode    = errors.New("this tx can't be used with this caminoGenesis.LockModeBondDeposit")
	errNoUTXOsForImport = errors.New("no utxos for import")
	errWrongOutType     = errors.New("wrong output type")
)

type CaminoBuilder interface {
	Builder
	CaminoTxBuilder
	utxo.Spender
}

type CaminoTxBuilder interface {
	NewCaminoAddValidatorTx(
		stakeAmount,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		nodeOwnerAddress ids.ShortID,
		rewardAddress ids.ShortID,
		shares uint32,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	NewAddressStateTx(
		address ids.ShortID,
		remove bool,
		state txs.AddressStateBit,
		keys []*secp256k1.PrivateKey,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewDepositTx(
		amount uint64,
		duration uint32,
		depositOfferID ids.ID,
		rewardAddress ids.ShortID,
		keys []*secp256k1.PrivateKey,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewUnlockDepositTx(
		depositTxIDs []ids.ID,
		keys []*secp256k1.PrivateKey,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewClaimTx(
		claimables []txs.ClaimAmount,
		claimTo *secp256k1fx.OutputOwners,
		keys []*secp256k1.PrivateKey,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewRegisterNodeTx(
		oldNodeID ids.NodeID,
		newNodeID ids.NodeID,
		nodeOwnerAddress ids.ShortID,
		keys []*secp256k1.PrivateKey,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewBaseTx(
		amount uint64,
		transferTo *secp256k1fx.OutputOwners,
		keys []*secp256k1.PrivateKey,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewRewardsImportTx() (*txs.Tx, error)

	NewSystemUnlockDepositTx(
		depositTxIDs []ids.ID,
	) (*txs.Tx, error)

	FinishProposalsTx(
		state state.Chain,
		earlyFinishedProposalIDs []ids.ID,
		expiredProposalIDs []ids.ID,
	) (*txs.Tx, error)
}

func NewCamino(
	ctx *snow.Context,
	cfg *config.Config,
	clk *mockable.Clock,
	fx fx.Fx,
	state state.State,
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

func (b *caminoBuilder) NewCaminoAddValidatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	nodeOwnerAddress ids.ShortID,
	rewardAddress ids.ShortID,
	shares uint32,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	caminoGenesis, err := b.state.CaminoConfig()
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

	ins, outs, signers, _, err := b.Lock(
		b.state,
		keys,
		stakeAmount,
		b.cfg.AddPrimaryNetworkValidatorFee,
		locked.StateBonded,
		nil,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		},
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	kc := secp256k1fx.NewKeychain(keys...)
	nodeOwnerInput, nodeOwnerSigners, err := kc.SpendMultiSig(
		&secp256k1fx.TransferOutput{
			OutputOwners: secp256k1fx.OutputOwners{
				Addrs:     []ids.ShortID{nodeOwnerAddress},
				Threshold: 1,
			},
		},
		0,
		b.state,
	)
	if err != nil {
		return nil, err
	}
	signers = append(signers, nodeOwnerSigners)

	utx := &txs.CaminoAddValidatorTx{
		AddValidatorTx: txs.AddValidatorTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    b.ctx.NetworkID,
				BlockchainID: b.ctx.ChainID,
				Ins:          ins,
				Outs:         outs,
			}},
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  startTime,
				End:    endTime,
				Wght:   stakeAmount,
			},
			RewardsOwner: &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{rewardAddress},
			},
		},
		NodeOwnerAuth: &nodeOwnerInput.(*secp256k1fx.TransferInput).Input,
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
	keys []*secp256k1.PrivateKey,
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

	if caminoGenesis, err := b.state.CaminoConfig(); err != nil {
		return nil, err
	} else if !caminoGenesis.VerifyNodeSignature {
		return tx, nil
	}

	nodeSigners, err := getSigner(keys, ids.ShortID(nodeID))
	if err != nil {
		return nil, err
	}

	if err := tx.Sign(txs.Codec, [][]*secp256k1.PrivateKey{nodeSigners}); err != nil {
		return nil, err
	}

	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewRewardValidatorTx(txID ids.ID) (*txs.Tx, error) {
	if state, err := b.state.CaminoConfig(); err != nil {
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

func (b *caminoBuilder) NewAddressStateTx(
	address ids.ShortID,
	remove bool,
	state txs.AddressStateBit,
	keys []*secp256k1.PrivateKey,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	ins, outs, signers, _, err := b.Lock(b.state, keys, 0, b.cfg.TxFee, locked.StateUnlocked, nil, change, 0)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the tx
	utx := &txs.AddressStateTx{
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
	keys []*secp256k1.PrivateKey,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	caminoGenesis, err := b.state.CaminoConfig()
	if err != nil {
		return nil, err
	}
	if !caminoGenesis.LockModeBondDeposit {
		return nil, errWrongLockMode
	}

	ins, outs, signers, _, err := b.Lock(b.state, keys, amount, b.cfg.TxFee, locked.StateDeposited, nil, change, 0)
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
	depositTxIDs []ids.ID,
	keys []*secp256k1.PrivateKey,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	caminoGenesis, err := b.state.CaminoConfig()
	if err != nil {
		return nil, err
	}
	if !caminoGenesis.LockModeBondDeposit {
		return nil, errWrongLockMode
	}

	// unlocking
	ins, outs, signers, err := b.UnlockDeposit(b.state, keys, depositTxIDs)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// burning fee
	feeIns, feeOuts, feeSigners, _, err := b.Lock(b.state, keys, 0, b.cfg.TxFee, locked.StateUnlocked, nil, change, 0)
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

func (b *caminoBuilder) NewClaimTx(
	claimables []txs.ClaimAmount,
	claimTo *secp256k1fx.OutputOwners,
	keys []*secp256k1.PrivateKey,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	caminoGenesis, err := b.state.CaminoConfig()
	if err != nil {
		return nil, err
	}
	if !caminoGenesis.LockModeBondDeposit {
		return nil, errWrongLockMode
	}

	ins, outs, signers, _, err := b.Lock(b.state, keys, 0, b.cfg.TxFee, locked.StateUnlocked, nil, change, 0)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	kc := secp256k1fx.NewKeychain(keys...)

	for i, txClaimable := range claimables {
		var owner *secp256k1fx.OutputOwners
		switch txClaimable.Type {
		case txs.ClaimTypeActiveDepositReward:
			deposit, err := b.state.GetDeposit(txClaimable.ID)
			if err != nil {
				return nil, err
			}
			rewardOwner, ok := deposit.RewardOwner.(*secp256k1fx.OutputOwners)
			if !ok {
				return nil, errNotSECPOwner
			}
			owner = rewardOwner

		case txs.ClaimTypeExpiredDepositReward, txs.ClaimTypeValidatorReward, txs.ClaimTypeAllTreasury:
			treasuryClaimable, err := b.state.GetClaimable(txClaimable.ID)
			if err != nil {
				return nil, fmt.Errorf("couldn't get claimable for ownerID %s: %w", txClaimable.ID, err)
			}
			owner = treasuryClaimable.Owner
		}

		claimableInput, claimableSigners, err := kc.SpendMultiSig(
			&secp256k1fx.TransferOutput{OutputOwners: *owner},
			0,
			b.state,
		)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", errKeyMissing, err)
		}

		signers = append(signers, claimableSigners)
		claimables[i].OwnerAuth = &claimableInput.(*secp256k1fx.TransferInput).Input

		outIntf, err := b.fx.CreateOutput(txClaimable.Amount, claimTo)
		if err != nil {
			return nil, fmt.Errorf("failed to create reward output: %w", err)
		}
		out, ok := outIntf.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, errWrongOutType
		}
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
			Out:   out,
		})
	}

	avax.SortTransferableOutputs(outs, txs.Codec)

	utx := &txs.ClaimTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Claimables: claimables,
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
	nodeOwnerAddress ids.ShortID,
	keys []*secp256k1.PrivateKey,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	ins, outs, signers, _, err := b.Lock(b.state, keys, 0, b.cfg.TxFee, locked.StateUnlocked, nil, change, 0)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	nodeSigners := []*secp256k1.PrivateKey{}
	if newNodeID != ids.EmptyNodeID {
		nodeSigners, err = getSigner(keys, ids.ShortID(newNodeID))
		if err != nil {
			return nil, err
		}
	}
	signers = append(signers, nodeSigners)

	kc := secp256k1fx.NewKeychain(keys...)
	in, consortiumSigners, err := kc.SpendMultiSig(
		&secp256k1fx.TransferOutput{
			OutputOwners: secp256k1fx.OutputOwners{
				Addrs:     []ids.ShortID{nodeOwnerAddress},
				Threshold: 1,
			},
		},
		0,
		b.state,
	)
	if err != nil {
		return nil, err
	}
	signers = append(signers, consortiumSigners)

	utx := &txs.RegisterNodeTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		OldNodeID:        oldNodeID,
		NewNodeID:        newNodeID,
		NodeOwnerAuth:    &in.(*secp256k1fx.TransferInput).Input,
		NodeOwnerAddress: nodeOwnerAddress,
	}

	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewBaseTx(
	amount uint64,
	transferTo *secp256k1fx.OutputOwners,
	keys []*secp256k1.PrivateKey,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	ins, outs, signers, _, err := b.Lock(b.state, keys, amount, b.cfg.TxFee, locked.StateUnlocked, transferTo, change, 0)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    b.ctx.NetworkID,
		BlockchainID: b.ctx.ChainID,
		Ins:          ins,
		Outs:         outs,
	}}

	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewRewardsImportTx() (*txs.Tx, error) {
	caminoGenesis, err := b.state.CaminoConfig()
	if err != nil {
		return nil, err
	}

	if !caminoGenesis.LockModeBondDeposit {
		return nil, errWrongLockMode
	}

	allUTXOsBytes, _, _, err := b.ctx.SharedMemory.Indexed(
		b.ctx.CChainID,
		treasury.AddrTraitsBytes,
		ids.ShortEmpty[:], ids.Empty[:], MaxPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("error fetching atomic UTXOs: %w", err)
	}

	now := b.clk.Unix()

	utxos := []*avax.UTXO{}
	for _, utxoBytes := range allUTXOsBytes {
		utxo := &avax.TimedUTXO{}
		if _, err := txs.Codec.Unmarshal(utxoBytes, utxo); err != nil {
			// that means that this could be simple, not-timed utxo
			continue
		}

		if utxo.Timestamp <= now-atomic.SharedMemorySyncBound {
			utxos = append(utxos, &utxo.UTXO)
		}
	}

	if len(utxos) == 0 {
		return nil, errNoUTXOsForImport
	}

	ins := make([]*avax.TransferableInput, len(utxos))

	for i, utxo := range utxos {
		inputIntf, _, err := fakeTreasuryKeychain.Spend(utxo.Out, now)
		if err != nil {
			return nil, err
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			return nil, err
		}
		ins[i] = &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In:     input,
		}
	}

	avax.SortTransferableInputs(ins)

	utx := &txs.RewardsImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
		}},
	}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}

	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) NewSystemUnlockDepositTx(
	depositTxIDs []ids.ID,
) (*txs.Tx, error) {
	ins, outs, err := b.Unlock(b.state, depositTxIDs, locked.StateDeposited)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx := &txs.UnlockDepositTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
	}

	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *caminoBuilder) FinishProposalsTx(
	state state.Chain,
	earlyFinishedProposalIDs []ids.ID,
	expiredProposalIDs []ids.ID,
) (*txs.Tx, error) {
	ins, outs, err := b.Unlock(
		state,
		append(earlyFinishedProposalIDs, expiredProposalIDs...),
		locked.StateBonded,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx := &txs.FinishProposalsTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    b.ctx.NetworkID,
		BlockchainID: b.ctx.ChainID,
		Ins:          ins,
		Outs:         outs,
	}}}

	for _, proposalID := range earlyFinishedProposalIDs {
		proposal, err := state.GetProposal(proposalID)
		if err != nil {
			return nil, fmt.Errorf("couldn't get proposal from state: %w", err)
		}
		if proposal.IsSuccessful() {
			utx.EarlyFinishedSuccessfulProposalIDs = append(utx.EarlyFinishedSuccessfulProposalIDs, proposalID)
		} else {
			utx.EarlyFinishedFailedProposalIDs = append(utx.EarlyFinishedFailedProposalIDs, proposalID)
		}
	}
	for _, proposalID := range expiredProposalIDs {
		proposal, err := state.GetProposal(proposalID)
		if err != nil {
			return nil, fmt.Errorf("couldn't get proposal from state: %w", err)
		}
		if proposal.IsSuccessful() {
			utx.ExpiredSuccessfulProposalIDs = append(utx.ExpiredSuccessfulProposalIDs, proposalID)
		} else {
			utx.ExpiredFailedProposalIDs = append(utx.ExpiredFailedProposalIDs, proposalID)
		}
	}

	utils.Sort(utx.EarlyFinishedSuccessfulProposalIDs)
	utils.Sort(utx.EarlyFinishedFailedProposalIDs)
	utils.Sort(utx.ExpiredSuccessfulProposalIDs)
	utils.Sort(utx.ExpiredFailedProposalIDs)

	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func getSigner(
	keys []*secp256k1.PrivateKey,
	address ids.ShortID,
) ([]*secp256k1.PrivateKey, error) {
	return getSigners(keys, []ids.ShortID{address})
}

func getSigners(
	keys []*secp256k1.PrivateKey,
	addresses []ids.ShortID,
) ([]*secp256k1.PrivateKey, error) {
	signers := make([]*secp256k1.PrivateKey, len(addresses))
	for i, addr := range addresses {
		signer, found := secp256k1fx.NewKeychain(keys...).Get(addr)
		if !found {
			return nil, fmt.Errorf("%w %s", errKeyMissing, addr.String())
		}

		key, ok := signer.(*secp256k1.PrivateKey)
		if !ok {
			return nil, errWrongNodeKeyType
		}

		signers[i] = key
	}
	return signers, nil
}
