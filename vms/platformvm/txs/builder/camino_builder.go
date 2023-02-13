// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ CaminoBuilder = (*caminoBuilder)(nil)

	fakeTreasuryKey      = crypto.FakePrivateKey(treasury.Addr)
	fakeTreasuryKeychain = secp256k1fx.NewKeychain(fakeTreasuryKey)

	errKeyMissing       = errors.New("couldn't find key matching address")
	errWrongNodeKeyType = errors.New("node key type isn't *crypto.PrivateKeySECP256K1R")
	errTxIsNotCommitted = errors.New("tx is not committed")
	errNotSECPOwner     = errors.New("owner is not *secp256k1fx.OutputOwners")
	errWrongTxType      = errors.New("wrong transaction type")
	errWrongOutType     = errors.New("wrong output type")
	errWrongLockMode    = errors.New("this tx can't be used with this caminoGenesis.LockModeBondDeposit")
	errNoUTXOsForImport = errors.New("no utxos for import")
)

type CaminoBuilder interface {
	Builder
	CaminoTxBuilder
	utxo.Spender
}

type CaminoTxBuilder interface {
	NewAddressStateTx(
		address ids.ShortID,
		remove bool,
		state uint8,
		keys []*crypto.PrivateKeySECP256K1R,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewDepositTx(
		amount uint64,
		duration uint32,
		depositOfferID ids.ID,
		rewardAddress ids.ShortID,
		keys []*crypto.PrivateKeySECP256K1R,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewUnlockDepositTx(
		lockTxIDs []ids.ID,
		keys []*crypto.PrivateKeySECP256K1R,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewClaimRewardTx(
		depositTxIDs []ids.ID,
		rewardAddress ids.ShortID,
		keys []*crypto.PrivateKeySECP256K1R,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewRegisterNodeTx(
		OldNodeID ids.NodeID,
		NewNodeID ids.NodeID,
		ConsortiumMemberAddress ids.ShortID,
		keys []*crypto.PrivateKeySECP256K1R,
		change *secp256k1fx.OutputOwners,
	) (*txs.Tx, error)

	NewRewardsImportTx() (*txs.Tx, error)
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

	ins, outs, signers, err := b.Lock(
		keys,
		stakeAmount,
		b.cfg.AddPrimaryNetworkValidatorFee,
		locked.StateBonded,
		nil,
		&secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		},
		0,
	)
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

	if caminoGenesis, err := b.state.CaminoConfig(); err != nil {
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
	state uint8,
	keys []*crypto.PrivateKeySECP256K1R,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	ins, outs, signers, err := b.Lock(keys, 0, b.cfg.TxFee, locked.StateUnlocked, nil, change, 0)
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
	keys []*crypto.PrivateKeySECP256K1R,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	caminoGenesis, err := b.state.CaminoConfig()
	if err != nil {
		return nil, err
	}
	if !caminoGenesis.LockModeBondDeposit {
		return nil, errWrongLockMode
	}

	ins, outs, signers, err := b.Lock(keys, amount, b.cfg.TxFee, locked.StateDeposited, nil, change, 0)
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
	ins, outs, signers, err := b.UnlockDeposit(b.state, keys, lockTxIDs)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// burning fee
	feeIns, feeOuts, feeSigners, err := b.Lock(keys, 0, b.cfg.TxFee, locked.StateUnlocked, nil, change, 0)
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

func (b *caminoBuilder) NewClaimRewardTx(
	depositTxIDs []ids.ID,
	rewardAddress ids.ShortID,
	keys []*crypto.PrivateKeySECP256K1R,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	caminoGenesis, err := b.state.CaminoConfig()
	if err != nil {
		return nil, err
	}
	if !caminoGenesis.LockModeBondDeposit {
		return nil, errWrongLockMode
	}

	ins, outs, signers, err := b.Lock(keys, 0, b.cfg.TxFee, locked.StateUnlocked, nil, change, 0)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	kc := secp256k1fx.NewKeychain(keys...)
	depositSignersKC := secp256k1fx.NewKeychain()

	for _, depositTxID := range depositTxIDs {
		depositRewardsOwner, err := getDepositRewardsOwner(b.state, depositTxID)
		if err != nil {
			return nil, err
		}

		_, signers, able := kc.Match(depositRewardsOwner, b.clk.Unix())
		if !able {
			return nil, errKeyMissing
		}

		for _, signer := range signers {
			depositSignersKC.Add(signer)
		}
	}

	signers = append(signers, depositSignersKC.Keys)

	utx := &txs.ClaimRewardTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		DepositTxs: depositTxIDs,
		RewardsOwner: &secp256k1fx.OutputOwners{
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

func (b *caminoBuilder) NewRegisterNodeTx(
	oldNodeID ids.NodeID,
	newNodeID ids.NodeID,
	consortiumMemberAddress ids.ShortID,
	keys []*crypto.PrivateKeySECP256K1R,
	change *secp256k1fx.OutputOwners,
) (*txs.Tx, error) {
	ins, outs, signers, err := b.Lock(keys, 0, b.cfg.TxFee, locked.StateUnlocked, nil, change, 0)
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

	consortiumMemberOwner, err := state.GetOwner(b.state, consortiumMemberAddress)
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

	// Calculating imported amount, getting utxoids

	utxoIDs := make([][]byte, len(utxos))
	importedAmount := uint64(0)
	for i, utxo := range utxos {
		utxoID := utxo.InputID()
		utxoIDs[i] = utxoID[:]
		secpOut, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, errWrongOutType
		}
		importedAmount, err = math.Add64(importedAmount, secpOut.Amt)
		if err != nil {
			return nil, fmt.Errorf("can't compact imported UTXOs: %w", err)
		}
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
		ImportedInputs: ins,
		Out: &avax.TransferableOutput{
			Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:          importedAmount,
				OutputOwners: *treasury.Owner,
			},
		},
	}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
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

func getDepositRewardsOwner(state state.Chain, depositTxID ids.ID) (*secp256k1fx.OutputOwners, error) {
	signedDepositTx, txStatus, err := state.GetTx(depositTxID)
	if err != nil {
		return nil, err
	}
	if txStatus != status.Committed {
		return nil, errTxIsNotCommitted
	}
	depositTx, ok := signedDepositTx.Unsigned.(*txs.DepositTx)
	if !ok {
		return nil, errWrongTxType
	}

	depositRewardsOwner, ok := depositTx.RewardsOwner.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, errNotSECPOwner
	}

	return depositRewardsOwner, nil
}
