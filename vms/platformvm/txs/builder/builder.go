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
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
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
		memo []byte,
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
		memo []byte,
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
		memo []byte,
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
		memo []byte,
	) (*txs.Tx, error)

	NewTransformSubnetTx(
		subnetID ids.ID,
		assetID ids.ID,
		initialSupply uint64,
		maxSupply uint64,
		minConsumptionRate uint64,
		maxConsumptionRate uint64,
		minValidatorStake uint64,
		maxValidatorStake uint64,
		minStakeDuration time.Duration,
		maxStakeDuration time.Duration,
		minDelegationFee uint32,
		minDelegatorStake uint64,
		maxValidatorWeightFactor byte,
		uptimeRequirement uint32,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
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
		memo []byte,
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
		memo []byte,
	) (*txs.Tx, error)

	// stakeAmount: amount the validator stakes
	// startTime: unix time they start validating
	// endTime: unix time they stop validating
	// nodeID: ID of the node we want to validate with
	// pop: the node proof of possession
	// rewardAddress: address to send reward to, if applicable
	// shares: 10,000 times percentage of reward taken from delegators
	// keys: Keys providing the staked tokens
	// changeAddr: Address to send change to, if there is any
	NewAddPermissionlessValidatorTx(
		stakeAmount,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		pop *signer.ProofOfPossession,
		rewardAddress ids.ShortID,
		shares uint32,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
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
		memo []byte,
	) (*txs.Tx, error)

	// stakeAmount: amount the delegator stakes
	// startTime: unix time they start delegating
	// endTime: unix time they stop delegating
	// nodeID: ID of the node we are delegating to
	// rewardAddress: address to send reward to, if applicable
	// keys: keys providing the staked tokens
	// changeAddr: address to send change to, if there is any
	NewAddPermissionlessDelegatorTx(
		stakeAmount,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		rewardAddress ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
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
		memo []byte,
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
		memo []byte,
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
		memo []byte,
	) (*txs.Tx, error)
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
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Memo:         memo,
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
		importedAVAX     = importedAmounts[b.ctx.AVAXAssetID] // the only entry left in importedAmounts
		chainTime        = b.state.GetTimestamp()
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()
		feeCalc     = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}

		ins []*avax.TransferableInput
	)

	// while outs are not ordered we add them to get current fees. We'll fix ordering later on
	utx.BaseTx.Outs = outs

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err = feeCalc.ImportTx(utx); err != nil {
		return nil, err
	}

	// account for imported inputs credentials
	for _, signer := range signers {
		credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, len(signer))
		if err != nil {
			return nil, fmt.Errorf("failed calculating credentials size: %w", err)
		}
		if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
			return nil, fmt.Errorf("account for credentials fees: %w", err)
		}
	}

	if feeCalc.Fee >= importedAVAX {
		// all imported avax will be burned to pay taxes.
		// Fees are scaled back accordingly.
		feeCalc.Fee -= importedAVAX
	} else {
		// imported inputs may be enough to pay taxes by themselves
		changeOut := &avax.TransferableOutput{
			Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{ // we set amount after considering changeOut own fees
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		}

		// update fees to target given the extra output added
		outDimensions, err := commonfees.GetOutputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableOutput{changeOut})
		if err != nil {
			return nil, fmt.Errorf("failed calculating output size: %w", err)
		}
		if _, err := feeCalc.AddFeesFor(outDimensions); err != nil {
			return nil, fmt.Errorf("account for output fees: %w", err)
		}

		if feeCalc.Fee >= importedAVAX {
			// imported avax are not enough to pay fees
			// Drop the changeOut and finance the tx
			if _, err := feeCalc.RemoveFeesFor(outDimensions); err != nil {
				return nil, fmt.Errorf("failed reverting change output: %w", err)
			}
			feeCalc.Fee -= importedAVAX
		} else {
			changeOut.Out.(*secp256k1fx.TransferOutput).Amt = importedAVAX - feeCalc.Fee
			feeCalc.Fee = 0
			outs = append(outs, changeOut)
		}
	}

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
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Memo:         memo,
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
		chainTime        = b.state.GetTimestamp()
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()

		feeCalc = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.ExportTx(utx); err != nil {
		return nil, err
	}
	feeCalc.Fee += amount // account for the transferred amount to be burned

	ins, outs, _, signers, err := b.FinanceTx(
		b.state,
		keys,
		0,
		feeCalc,
		changeAddr,
	)
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
	memo []byte,
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
			Memo:         memo,
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
		chainTime        = b.state.GetTimestamp()
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()
		feeCalc     = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err = feeCalc.CreateChainTx(utx); err != nil {
		return nil, err
	}

	// account for subnet authorization credentials
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, len(subnetSigners))
	if err != nil {
		return nil, fmt.Errorf("failed calculating credentials size: %w", err)
	}
	if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
		return nil, fmt.Errorf("account for credentials fees: %w", err)
	}

	ins, outs, _, signers, err := b.FinanceTx(
		b.state,
		keys,
		0,
		feeCalc,
		changeAddr,
	)
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
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utils.Sort(ownerAddrs) // sort control addresses

	utx := &txs.CreateSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Memo:         memo,
		}},
		Owner: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     ownerAddrs,
		},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime        = b.state.GetTimestamp()
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()

		feeCalc = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.CreateSubnetTx(utx); err != nil {
		return nil, err
	}

	ins, outs, _, signers, err := b.FinanceTx(
		b.state,
		keys,
		0,
		feeCalc,
		changeAddr,
	)
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

func (b *builder) NewTransformSubnetTx(
	subnetID ids.ID,
	assetID ids.ID,
	initialSupply uint64,
	maxSupply uint64,
	minConsumptionRate uint64,
	maxConsumptionRate uint64,
	minValidatorStake uint64,
	maxValidatorStake uint64,
	minStakeDuration time.Duration,
	maxStakeDuration time.Duration,
	minDelegationFee uint32,
	minDelegatorStake uint64,
	maxValidatorWeightFactor byte,
	uptimeRequirement uint32,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	subnetAuth, subnetSigners, err := b.Authorize(b.state, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}

	utx := &txs.TransformSubnetTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    b.ctx.NetworkID,
				BlockchainID: b.ctx.ChainID,
				Memo:         memo,
			},
		},
		Subnet:                   subnetID,
		AssetID:                  assetID,
		InitialSupply:            initialSupply,
		MaximumSupply:            maxSupply,
		MinConsumptionRate:       minConsumptionRate,
		MaxConsumptionRate:       maxConsumptionRate,
		MinValidatorStake:        minValidatorStake,
		MaxValidatorStake:        maxValidatorStake,
		MinStakeDuration:         uint32(minStakeDuration / time.Second),
		MaxStakeDuration:         uint32(maxStakeDuration / time.Second),
		MinDelegationFee:         minDelegationFee,
		MinDelegatorStake:        minDelegatorStake,
		MaxValidatorWeightFactor: maxValidatorWeightFactor,
		UptimeRequirement:        uptimeRequirement,
		SubnetAuth:               subnetAuth,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime        = b.state.GetTimestamp()
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()
		feeCalc     = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.TransformSubnetTx(utx); err != nil {
		return nil, err
	}

	// account for subnet authorization credentials
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, len(subnetSigners))
	if err != nil {
		return nil, fmt.Errorf("failed calculating credentials size: %w", err)
	}
	if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
		return nil, fmt.Errorf("account for credentials fees: %w", err)
	}

	ins, outs, _, signers, err := b.FinanceTx(
		b.state,
		keys,
		0,
		feeCalc,
		changeAddr,
	)
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

func (b *builder) NewAddValidatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	shares uint32,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.AddValidatorTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID: b.ctx.NetworkID,
				Memo:      memo,
			},
		},
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
		chainTime        = b.state.GetTimestamp()
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()
		feeCalc     = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.AddValidatorTx(utx); err != nil {
		return nil, err
	}

	ins, outs, stakeOuts, signers, err := b.FinanceTx(
		b.state,
		keys,
		stakeAmount,
		feeCalc,
		changeAddr,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs
	utx.StakeOuts = stakeOuts

	// 3. Sign the tx
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddPermissionlessValidatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	pop *signer.ProofOfPossession,
	rewardAddress ids.ShortID,
	shares uint32,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.AddPermissionlessValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Memo:         memo,
		}},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  startTime,
			End:    endTime,
			Wght:   stakeAmount,
		},
		Subnet: constants.PrimaryNetworkID,
		Signer: pop,
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		DelegationShares: shares,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime        = b.state.GetTimestamp()
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()

		feeCalc = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.AddPermissionlessValidatorTx(utx); err != nil {
		return nil, err
	}

	ins, outs, stakeOuts, signers, err := b.FinanceTx(
		b.state,
		keys,
		stakeAmount,
		feeCalc,
		changeAddr,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs
	utx.StakeOuts = stakeOuts

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
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.AddDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Memo:         memo,
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
		chainTime        = b.state.GetTimestamp()
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()

		feeCalc = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.AddDelegatorTx(utx); err != nil {
		return nil, err
	}

	ins, outs, stakeOuts, signers, err := b.FinanceTx(
		b.state,
		keys,
		stakeAmount,
		feeCalc,
		changeAddr,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs
	utx.StakeOuts = stakeOuts

	// 3. Sign the tx
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddPermissionlessDelegatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.AddPermissionlessDelegatorTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    b.ctx.NetworkID,
				BlockchainID: b.ctx.ChainID,
				Memo:         memo,
			},
		},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  startTime,
			End:    endTime,
			Wght:   stakeAmount,
		},
		Subnet: constants.PrimaryNetworkID,
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime        = b.state.GetTimestamp()
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()

		feeCalc = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.AddPermissionlessDelegatorTx(utx); err != nil {
		return nil, err
	}

	ins, outs, stakeOuts, signers, err := b.FinanceTx(
		b.state,
		keys,
		stakeAmount,
		feeCalc,
		changeAddr,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.BaseTx.Outs = outs
	utx.StakeOuts = stakeOuts

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
	memo []byte,
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
			Memo:         memo,
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
		chainTime        = b.state.GetTimestamp()
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()
		feeCalc     = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err = feeCalc.AddSubnetValidatorTx(utx); err != nil {
		return nil, err
	}

	// account for subnet authorization credentials
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, len(subnetSigners))
	if err != nil {
		return nil, fmt.Errorf("failed calculating credentials size: %w", err)
	}
	if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
		return nil, fmt.Errorf("account for credentials fees: %w", err)
	}

	ins, outs, _, signers, err := b.FinanceTx(
		b.state,
		keys,
		weight,
		feeCalc,
		changeAddr,
	)
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
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	subnetAuth, subnetSigners, err := b.Authorize(b.state, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	utx := &txs.RemoveSubnetValidatorTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    b.ctx.NetworkID,
				BlockchainID: b.ctx.ChainID,
				Memo:         memo,
			},
		},
		Subnet:     subnetID,
		NodeID:     nodeID,
		SubnetAuth: subnetAuth,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime        = b.state.GetTimestamp()
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()
		feeCalc     = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err = feeCalc.RemoveSubnetValidatorTx(utx); err != nil {
		return nil, err
	}

	// account for subnet authorization credentials
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, len(subnetSigners))
	if err != nil {
		return nil, fmt.Errorf("failed calculating credentials size: %w", err)
	}
	if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
		return nil, fmt.Errorf("account for credentials fees: %w", err)
	}

	ins, outs, _, signers, err := b.FinanceTx(
		b.state,
		keys,
		0,
		feeCalc,
		changeAddr,
	)
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

func (b *builder) NewTransferSubnetOwnershipTx(
	subnetID ids.ID,
	threshold uint32,
	ownerAddrs []ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	subnetAuth, subnetSigners, err := b.Authorize(b.state, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	utx := &txs.TransferSubnetOwnershipTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    b.ctx.NetworkID,
				BlockchainID: b.ctx.ChainID,
				Memo:         memo,
			},
		},
		Subnet:     subnetID,
		SubnetAuth: subnetAuth,
		Owner: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     ownerAddrs,
		},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime        = b.state.GetTimestamp()
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()
		feeCalc     = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err = feeCalc.TransferSubnetOwnershipTx(utx); err != nil {
		return nil, err
	}

	// account for subnet authorization credentials
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, len(subnetSigners))
	if err != nil {
		return nil, fmt.Errorf("failed calculating credentials size: %w", err)
	}
	if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
		return nil, fmt.Errorf("account for credentials fees: %w", err)
	}

	ins, outs, _, signers, err := b.FinanceTx(
		b.state,
		keys,
		0,
		feeCalc,
		changeAddr,
	)
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
	memo []byte,
) (*txs.Tx, error) {
	// 1. Build core transaction without utxos
	utx := &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt:          amount,
					OutputOwners: owner,
				},
			}}, // not sorted yet, we'll sort later on when we have all the outputs
			Memo: memo,
		},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	var (
		chainTime        = b.state.GetTimestamp()
		isEUpgradeActive = b.cfg.IsEUpgradeActivated(chainTime)
		feeCfg           = b.cfg.GetDynamicFeesConfig(chainTime)

		unitFees    = b.state.GetUnitFees()
		unitWindows = b.state.GetFeeWindows()
		feeCalc     = &fees.Calculator{
			IsEUpgradeActive: isEUpgradeActive,
			Config:           b.cfg,
			ChainTime:        chainTime,
			FeeManager:       commonfees.NewManager(unitFees, unitWindows),
			ConsumedUnitsCap: feeCfg.BlockUnitsCap,
		}
	)

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.BaseTx(utx); err != nil {
		return nil, err
	}
	feeCalc.Fee += amount // account for the transferred amount to be burned

	ins, outs, _, signers, err := b.FinanceTx(
		b.state,
		keys,
		0,
		feeCalc,
		changeAddr,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	utx.BaseTx.Ins = ins
	utx.Outs = append(utx.Outs, outs...)
	avax.SortTransferableOutputs(utx.Outs, txs.Codec)

	// 3. Sign the tx
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}
