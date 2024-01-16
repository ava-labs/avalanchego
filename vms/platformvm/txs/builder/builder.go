// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

// Max number of items allowed in a page
const MaxPageSize = 1024

var (
	_ Builder = (*builder)(nil)

	ErrNoFunds = errors.New("no spendable funds were found")
)

type Builder interface {
	AtomicTxBuilder
	DecisionTxBuilder
	ProposalTxBuilder
}

type AtomicTxBuilder interface {
	// chainID: chain to import UTXOs from
	// to: address of recipient
	// keys: keys to import the funds
	// changeAddr: address to send change to, if there is any
	NewImportTx(
		chainID ids.ID,
		to ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	// amount: amount of tokens to export
	// chainID: chain to send the UTXOs to
	// to: address of recipient
	// keys: keys to pay the fee and provide the tokens
	// changeAddr: address to send change to, if there is any
	NewExportTx(
		amount uint64,
		chainID ids.ID,
		to ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)
}

type DecisionTxBuilder interface {
	// subnetID: ID of the subnet that validates the new chain
	// genesisData: byte repr. of genesis state of the new chain
	// vmID: ID of VM this chain runs
	// fxIDs: ids of features extensions this chain supports
	// chainName: name of the chain
	// keys: keys to sign the tx
	// changeAddr: address to send change to, if there is any
	NewCreateChainTx(
		subnetID ids.ID,
		genesisData []byte,
		vmID ids.ID,
		fxIDs []ids.ID,
		chainName string,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	// threshold: [threshold] of [ownerAddrs] needed to manage this subnet
	// ownerAddrs: control addresses for the new subnet
	// keys: keys to pay the fee
	// changeAddr: address to send change to, if there is any
	NewCreateSubnetTx(
		threshold uint32,
		ownerAddrs []ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	// amount: amount the sender is sending
	// owner: recipient of the funds
	// keys: keys to sign the tx and pay the amount
	// changeAddr: address to send change to, if there is any
	NewBaseTx(
		amount uint64,
		owner secp256k1fx.OutputOwners,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)
}

type ProposalTxBuilder interface {
	// stakeAmount: amount the validator stakes
	// startTime: unix time they start validating
	// endTime: unix time they stop validating
	// nodeID: ID of the node we want to validate with
	// rewardAddress: address to send reward to, if applicable
	// shares: 10,000 times percentage of reward taken from delegators
	// keys: Keys providing the staked tokens
	// changeAddr: Address to send change to, if there is any
	NewAddValidatorTx(
		stakeAmount,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		rewardAddress ids.ShortID,
		shares uint32,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	// stakeAmount: amount the delegator stakes
	// startTime: unix time they start delegating
	// endTime: unix time they stop delegating
	// nodeID: ID of the node we are delegating to
	// rewardAddress: address to send reward to, if applicable
	// keys: keys providing the staked tokens
	// changeAddr: address to send change to, if there is any
	NewAddDelegatorTx(
		stakeAmount,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		rewardAddress ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	// weight: sampling weight of the new validator
	// startTime: unix time they start delegating
	// endTime:  unix time they top delegating
	// nodeID: ID of the node validating
	// subnetID: ID of the subnet the validator will validate
	// keys: keys to use for adding the validator
	// changeAddr: address to send change to, if there is any
	NewAddSubnetValidatorTx(
		weight,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		subnetID ids.ID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	// Creates a transaction that removes [nodeID]
	// as a validator from [subnetID]
	// keys: keys to use for removing the validator
	// changeAddr: address to send change to, if there is any
	NewRemoveSubnetValidatorTx(
		nodeID ids.NodeID,
		subnetID ids.ID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	// Creates a transaction that transfers ownership of [subnetID]
	// threshold: [threshold] of [ownerAddrs] needed to manage this subnet
	// ownerAddrs: control addresses for the new subnet
	// keys: keys to use for modifying the subnet
	// changeAddr: address to send change to, if there is any
	NewTransferSubnetOwnershipTx(
		subnetID ids.ID,
		threshold uint32,
		ownerAddrs []ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
	) (*txs.Tx, error)

	// newAdvanceTimeTx creates a new tx that, if it is accepted and followed by a
	// Commit block, will set the chain's timestamp to [timestamp].
	NewAdvanceTimeTx(timestamp time.Time) (*txs.Tx, error)

	// RewardStakerTx creates a new transaction that proposes to remove the staker
	// [validatorID] from the default validator set.
	NewRewardValidatorTx(txID ids.ID) (*txs.Tx, error)
}

func New(
	ctx *snow.Context,
	cfg *config.Config,
	clk *mockable.Clock,
	fx fx.Fx,
	state state.State,
	atomicUTXOManager avax.AtomicUTXOManager,
	utxoSpender utxo.Spender,
) Builder {
	return &builder{
		AtomicUTXOManager: atomicUTXOManager,
		Spender:           utxoSpender,
		state:             state,
		cfg:               cfg,
		ctx:               ctx,
		clk:               clk,
		fx:                fx,
	}
}

type builder struct {
	avax.AtomicUTXOManager
	utxo.Spender
	state state.State

	cfg *config.Config
	ctx *snow.Context
	clk *mockable.Clock
	fx  fx.Fx
}

func (b *builder) NewImportTx(
	from ids.ID,
	to ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		}},
		SourceChain: from,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	kc := secp256k1fx.NewKeychain(keys...)

	atomicUTXOs, _, _, err := b.GetAtomicUTXOs(from, kc.Addresses(), ids.ShortEmpty, ids.Empty, MaxPageSize)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	var (
		importedInputs = []*avax.TransferableInput{}
		signers        = [][]*secp256k1.PrivateKey{}
		outs           = []*avax.TransferableOutput{}

		importedAmounts = make(map[ids.ID]uint64)
		now             = b.clk.Unix()
	)
	for _, utxo := range atomicUTXOs {
		inputIntf, utxoSigners, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			continue
		}
		assetID := utxo.AssetID()
		importedAmounts[assetID], err = math.Add64(importedAmounts[assetID], input.Amount())
		if err != nil {
			return nil, err
		}
		importedInputs = append(importedInputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In:     input,
		})
		signers = append(signers, utxoSigners)
	}
	if len(importedAmounts) == 0 {
		return nil, ErrNoFunds // No imported UTXOs were spendable
	}

	// Sort and add imported txs to utx. Imported txs must not be
	// changed here in after
	avax.SortTransferableInputsWithSigners(importedInputs, signers)
	utx.ImportedInputs = importedInputs

	// add non avax-denominated outputs. Avax-denominated utxos
	// are used to pay fees whose amount is calculated later on
	for assetID, amount := range importedAmounts {
		if assetID == b.ctx.AVAXAssetID {
			continue
		}
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		})
		delete(importedAmounts, assetID)
	}

	var (
		ins []*avax.TransferableInput

		importedAVAX = importedAmounts[b.ctx.AVAXAssetID] // the only entry left in importedAmounts
		chainTime    = b.state.GetTimestamp()
	)
	if b.cfg.IsEForkActivated(chainTime) {
		// while outs are not ordered we add them to get current fees. We'll fix ordering later on
		utx.BaseTx.Outs = outs
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err = feeCalc.ImportTx(utx); err != nil {
			return nil, err
		}

		if feeCalc.Fee >= importedAVAX {
			// all imported avax will be burned to pay taxes.
			// Fees are scaled back accordingly.
			feeCalc.Fee -= importedAVAX
		} else {
			// imported inputs may be enough to pay taxes by themselves
			changeOut := &avax.TransferableOutput{
				Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					// Amt: importedAVAX, // SET IT AFTER CONSIDERING ITS OWN FEES
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{to},
					},
				},
			}

			// update fees to target given the extra input added
			outDimensions, err := fees.GetOutputsDimensions(changeOut)
			if err != nil {
				return nil, fmt.Errorf("failed calculating output size: %w", err)
			}
			if err := feeCalc.AddFeesFor(outDimensions); err != nil {
				return nil, fmt.Errorf("account for output fees: %w", err)
			}

			if feeCalc.Fee >= importedAVAX {
				// imported avax are not enough to pay fees
				// Drop the changeOut and finance the tx
				if err := feeCalc.RemoveFeesFor(outDimensions); err != nil {
					return nil, fmt.Errorf("failed reverting change output: %w", err)
				}
				feeCalc.Fee -= importedAVAX

				var (
					financeOut    []*avax.TransferableOutput
					financeSigner [][]*secp256k1.PrivateKey
				)
				ins, financeOut, _, financeSigner, err = b.FinanceTx(
					b.state,
					keys,
					0,
					feeCalc,
					changeAddr,
				)
				if err != nil {
					return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
				}
				outs = append(financeOut, outs...)
				signers = append(financeSigner, signers...)
			} else {
				changeOut.Out.(*secp256k1fx.TransferOutput).Amt = importedAVAX - feeCalc.Fee
				outs = append(outs, changeOut)
			}
		}
	} else {
		switch {
		case importedAVAX < b.cfg.TxFee: // imported amount goes toward paying tx fee
			var (
				baseOuts    []*avax.TransferableOutput
				baseSigners [][]*secp256k1.PrivateKey
			)
			ins, baseOuts, _, baseSigners, err = b.Spend(b.state, keys, 0, b.cfg.TxFee-importedAVAX, changeAddr)
			if err != nil {
				return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
			}
			outs = append(baseOuts, outs...)
			signers = append(baseSigners, signers...)
			delete(importedAmounts, b.ctx.AVAXAssetID)
		case importedAVAX == b.cfg.TxFee:
			delete(importedAmounts, b.ctx.AVAXAssetID)
		default:
			importedAVAX -= b.cfg.TxFee
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: importedAVAX,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{to},
					},
				},
			})
		}
	}

	avax.SortTransferableOutputs(outs, txs.Codec) // sort imported outputs

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs

	// 3. Sign the tx
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

// TODO: should support other assets than AVAX
func (b *builder) NewExportTx(
	amount uint64,
	chainID ids.ID,
	to ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		}},
		DestinationChain: chainID,
		ExportedOutputs: []*avax.TransferableOutput{{ // Exported to X-Chain
			Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		}},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime = b.state.GetTimestamp()
		ins       []*avax.TransferableInput
		outs      []*avax.TransferableOutput
		signers   [][]*secp256k1.PrivateKey
		err       error
	)
	if b.cfg.IsEForkActivated(chainTime) {
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err = feeCalc.ExportTx(utx); err != nil {
			return nil, err
		}

		ins, outs, _, signers, err = b.FinanceTx(
			b.state,
			keys,
			0,
			feeCalc,
			changeAddr,
		)
	} else {
		var toBurn uint64
		toBurn, err = math.Add64(amount, b.cfg.TxFee)
		if err != nil {
			return nil, fmt.Errorf("amount (%d) + tx fee(%d) overflows", amount, b.cfg.TxFee)
		}
		ins, outs, _, signers, err = b.Spend(b.state, keys, 0, toBurn, changeAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs

	// 3. Sign the tx
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewCreateChainTx(
	subnetID ids.ID,
	genesisData []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	subnetAuth, subnetSigners, err := b.Authorize(b.state, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}

	utils.Sort(fxIDs) // sort the provided fxIDs

	utx := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		}},
		SubnetID:    subnetID,
		ChainName:   chainName,
		VMID:        vmID,
		FxIDs:       fxIDs,
		GenesisData: genesisData,
		SubnetAuth:  subnetAuth,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime = b.state.GetTimestamp()
		ins       []*avax.TransferableInput
		outs      []*avax.TransferableOutput
		signers   [][]*secp256k1.PrivateKey
	)
	if b.cfg.IsEForkActivated(chainTime) {
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err = feeCalc.CreateChainTx(utx); err != nil {
			return nil, err
		}

		ins, outs, _, signers, err = b.FinanceTx(
			b.state,
			keys,
			0,
			feeCalc,
			changeAddr,
		)
	} else {
		timestamp := b.state.GetTimestamp()
		createBlockchainTxFee := b.cfg.GetCreateBlockchainTxFee(timestamp)
		ins, outs, _, signers, err = b.Spend(b.state, keys, 0, createBlockchainTxFee, changeAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs

	// 3. Sign the tx
	signers = append(signers, subnetSigners)
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewCreateSubnetTx(
	threshold uint32,
	ownerAddrs []ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utils.Sort(ownerAddrs) // sort control addresses

	utx := &txs.CreateSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		}},
		Owner: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     ownerAddrs,
		},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime = b.state.GetTimestamp()
		ins       []*avax.TransferableInput
		outs      []*avax.TransferableOutput
		signers   [][]*secp256k1.PrivateKey
		err       error
	)
	if b.cfg.IsEForkActivated(chainTime) {
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err := feeCalc.CreateSubnetTx(utx); err != nil {
			return nil, err
		}

		ins, outs, _, signers, err = b.FinanceTx(
			b.state,
			keys,
			0,
			feeCalc,
			changeAddr,
		)
	} else {
		createSubnetTxFee := b.cfg.GetCreateSubnetTxFee(chainTime)
		ins, outs, _, signers, err = b.Spend(b.state, keys, 0, createSubnetTxFee, changeAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs

	// 3. Sign the tx
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddValidatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	shares uint32,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.AddValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		}},
		Validator: txs.Validator{
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
		DelegationShares: shares,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime  = b.state.GetTimestamp()
		ins        []*avax.TransferableInput
		outs       []*avax.TransferableOutput
		stakedOuts []*avax.TransferableOutput
		signers    [][]*secp256k1.PrivateKey
		err        error
	)
	if b.cfg.IsEForkActivated(chainTime) {
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err = feeCalc.AddValidatorTx(utx); err != nil {
			return nil, err
		}

		ins, outs, _, signers, err = b.FinanceTx(
			b.state,
			keys,
			0,
			feeCalc,
			changeAddr,
		)
	} else {
		ins, outs, stakedOuts, signers, err = b.Spend(b.state, keys, stakeAmount, b.cfg.AddPrimaryNetworkValidatorFee, changeAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs
	utx.StakeOuts = stakedOuts

	// 3. Sign the tx
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddDelegatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.AddDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		}},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  startTime,
			End:    endTime,
			Wght:   stakeAmount,
		},
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime  = b.state.GetTimestamp()
		ins        []*avax.TransferableInput
		outs       []*avax.TransferableOutput
		stakedOuts []*avax.TransferableOutput
		signers    [][]*secp256k1.PrivateKey
		err        error
	)
	if b.cfg.IsEForkActivated(chainTime) {
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err = feeCalc.AddDelegatorTx(utx); err != nil {
			return nil, err
		}

		ins, outs, _, signers, err = b.FinanceTx(
			b.state,
			keys,
			0,
			feeCalc,
			changeAddr,
		)
	} else {
		ins, outs, stakedOuts, signers, err = b.Spend(b.state, keys, stakeAmount, b.cfg.AddPrimaryNetworkDelegatorFee, changeAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs
	utx.StakeOuts = stakedOuts

	// 3. Sign the tx
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddSubnetValidatorTx(
	weight,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	subnetID ids.ID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	subnetAuth, subnetSigners, err := b.Authorize(b.state, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	utx := &txs.AddSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		}},
		SubnetValidator: txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  startTime,
				End:    endTime,
				Wght:   weight,
			},
			Subnet: subnetID,
		},
		SubnetAuth: subnetAuth,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime = b.state.GetTimestamp()
		ins       []*avax.TransferableInput
		outs      []*avax.TransferableOutput
		signers   [][]*secp256k1.PrivateKey
	)
	if b.cfg.IsEForkActivated(chainTime) {
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err = feeCalc.AddSubnetValidatorTx(utx); err != nil {
			return nil, err
		}

		ins, outs, _, signers, err = b.FinanceTx(
			b.state,
			keys,
			0,
			feeCalc,
			changeAddr,
		)
	} else {
		ins, outs, _, signers, err = b.Spend(b.state, keys, 0, b.cfg.TxFee, changeAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs

	// 3. Sign the tx
	signers = append(signers, subnetSigners)
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	subnetAuth, subnetSigners, err := b.Authorize(b.state, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	utx := &txs.RemoveSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		}},
		Subnet:     subnetID,
		NodeID:     nodeID,
		SubnetAuth: subnetAuth,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime = b.state.GetTimestamp()
		ins       []*avax.TransferableInput
		outs      []*avax.TransferableOutput
		signers   [][]*secp256k1.PrivateKey
	)
	if b.cfg.IsEForkActivated(chainTime) {
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err = feeCalc.RemoveSubnetValidatorTx(utx); err != nil {
			return nil, err
		}

		ins, outs, _, signers, err = b.FinanceTx(
			b.state,
			keys,
			0,
			feeCalc,
			changeAddr,
		)
	} else {
		ins, outs, _, signers, err = b.Spend(b.state, keys, 0, b.cfg.TxFee, changeAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs

	// 3. Sign the tx
	signers = append(signers, subnetSigners)
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAdvanceTimeTx(timestamp time.Time) (*txs.Tx, error) {
	utx := &txs.AdvanceTimeTx{Time: uint64(timestamp.Unix())}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewRewardValidatorTx(txID ids.ID) (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{TxID: txID}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}

	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewTransferSubnetOwnershipTx(
	subnetID ids.ID,
	threshold uint32,
	ownerAddrs []ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	subnetAuth, subnetSigners, err := b.Authorize(b.state, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	utx := &txs.TransferSubnetOwnershipTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		}},
		Subnet:     subnetID,
		SubnetAuth: subnetAuth,
		Owner: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     ownerAddrs,
		},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime = b.state.GetTimestamp()
		ins       []*avax.TransferableInput
		outs      []*avax.TransferableOutput
		signers   [][]*secp256k1.PrivateKey
	)
	if b.cfg.IsEForkActivated(chainTime) {
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err = feeCalc.TransferSubnetOwnershipTx(utx); err != nil {
			return nil, err
		}

		ins, outs, _, signers, err = b.FinanceTx(
			b.state,
			keys,
			0,
			feeCalc,
			changeAddr,
		)
	} else {
		ins, outs, _, signers, err = b.Spend(b.state, keys, 0, b.cfg.TxFee, changeAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs

	// 3. Sign the tx
	signers = append(signers, subnetSigners)
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewBaseTx(
	amount uint64,
	owner secp256k1fx.OutputOwners,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
		},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime = b.state.GetTimestamp()
		ins       []*avax.TransferableInput
		outs      []*avax.TransferableOutput
		signers   [][]*secp256k1.PrivateKey
		err       error
	)
	if b.cfg.IsEForkActivated(chainTime) {
		feesMan := commonfees.NewManager(b.cfg.DefaultUnitFees)
		feeCalc := &fees.Calculator{
			FeeManager:  feesMan,
			Config:      b.cfg,
			ChainTime:   b.state.GetTimestamp(),
			Credentials: txs.EmptyCredentials(signers),
		}

		// feesMan cumulates consumed units. Let's init it with utx filled so far
		if err = feeCalc.BaseTx(utx); err != nil {
			return nil, err
		}

		ins, outs, _, signers, err = b.FinanceTx(
			b.state,
			keys,
			0,
			feeCalc,
			changeAddr,
		)
	} else {
		var toBurn uint64
		toBurn, err = math.Add64(amount, b.cfg.TxFee)
		if err != nil {
			return nil, fmt.Errorf("amount (%d) + tx fee(%d) overflows", amount, b.cfg.TxFee)
		}
		ins, outs, _, signers, err = b.Spend(b.state, keys, 0, toBurn, changeAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	outs = append(outs, &avax.TransferableOutput{
		Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          amount,
			OutputOwners: owner,
		},
	})
	avax.SortTransferableOutputs(outs, txs.Codec)

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs

	// 3. Sign the tx
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}
