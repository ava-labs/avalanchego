// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/timed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"

	p_utils "github.com/ava-labs/avalanchego/vms/platformvm/utils"
)

var (
	_ Executor = &executor{}

	ErrOverDelegated             = errors.New("validator would be over delegated")
	ErrWeightTooLarge            = errors.New("weight of this validator is too large")
	ErrStakeTooShort             = errors.New("staking period is too short")
	ErrStakeTooLong              = errors.New("staking period is too long")
	ErrFutureStakeTime           = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	ErrInsufficientDelegationFee = errors.New("staker charges an insufficient delegation fee")
	ErrInvalidID                 = errors.New("invalid ID")
	ErrShouldBeDSValidator       = errors.New("expected validator to be in the primary network")
)

const (
	// maxValidatorWeightFactor is the maximum factor of the validator stake
	// that is allowed to be placed on a validator.
	maxValidatorWeightFactor uint64 = 5

	// Maximum future start time for staking/delegating
	MaxFutureStartTime = 24 * 7 * 2 * time.Hour

	// SyncBound is the synchrony bound used for safe decision making
	SyncBound = 10 * time.Second
)

type Executor interface {
	// Attempts to verify this transaction with the provided txstate.
	SemanticVerify(
		stx *signed.Tx,
		parentState state.Mutable,
	) error

	ProposalExecutor

	ExecuteDecision(
		stx *signed.Tx,
		vs state.Versioned,
	) (func() error, error)

	ExecuteAtomicTx(
		stx *signed.Tx,
		vs state.Versioned,
	) (func() error, error)
}

func NewExecutor(
	cfg *config.Config,
	ctx *snow.Context,
	bootstrapped *utils.AtomicBool,
	clk *mockable.Clock,
	fx fx.Fx,
	utxosMan utxos.SpendHandler,
	timeMan uptime.Manager,
	rewards reward.Calculator,
) Executor {
	components := &components{
		cfg:          cfg,
		ctx:          ctx,
		bootstrapped: bootstrapped,
		clk:          clk,
		fx:           fx,
		spendHandler: utxosMan,
		uptimeMan:    timeMan,
		rewards:      rewards,
	}

	return &executor{
		proposalExecutor: proposalExecutor{
			components: components,
		},
		components: components,
	}
}

type components struct {
	cfg          *config.Config
	ctx          *snow.Context
	bootstrapped *utils.AtomicBool
	clk          *mockable.Clock
	fx           fx.Fx
	spendHandler utxos.SpendHandler
	uptimeMan    uptime.Manager
	rewards      reward.Calculator
}

type executor struct {
	proposalExecutor
	*components
}

// Attempts to verify this transaction with the provided txstate.
func (e *executor) SemanticVerify(
	stx *signed.Tx,
	parentState state.Mutable,
) error {
	switch utx := stx.Unsigned.(type) {
	case *unsigned.AddDelegatorTx,
		*unsigned.AddValidatorTx,
		*unsigned.AddSubnetValidatorTx:
		startTime := utx.(timed.Tx).StartTime()
		maxLocalStartTime := e.clk.Time().Add(MaxFutureStartTime)
		if startTime.After(maxLocalStartTime) {
			return ErrFutureStakeTime
		}

		_, _, err := e.ExecuteProposal(stx, parentState)
		// We ignore [errFutureStakeTime] here because an advanceTimeTx will be
		// issued before this transaction is issued.
		if errors.Is(err, ErrFutureStakeTime) {
			return nil
		}
		return err

	case *unsigned.AdvanceTimeTx,
		*unsigned.RewardValidatorTx:
		_, _, err := e.ExecuteProposal(stx, parentState)
		return err

	case *unsigned.CreateChainTx,
		*unsigned.CreateSubnetTx:
		vs := state.NewVersioned(
			parentState,
			parentState.CurrentStakerChainState(),
			parentState.PendingStakerChainState(),
		)
		_, err := e.ExecuteDecision(stx, vs)
		return err

	case *unsigned.ExportTx,
		*unsigned.ImportTx:
		vs := state.NewVersioned(
			parentState,
			parentState.CurrentStakerChainState(),
			parentState.PendingStakerChainState(),
		)
		_, err := e.ExecuteAtomicTx(stx, vs)
		return err

	default:
		return fmt.Errorf("tx type %T could not be semantically verified", utx)
	}
}

func (e *executor) ExecuteDecision(
	stx *signed.Tx,
	vs state.Versioned,
) (func() error, error) {
	var (
		txID        = stx.ID()
		creds       = stx.Creds
		signedBytes = stx.Bytes()
	)

	switch utx := stx.Unsigned.(type) {
	case *unsigned.CreateChainTx:
		return e.executeCreateChain(vs, utx, txID, signedBytes, creds)
	case *unsigned.CreateSubnetTx:
		return e.executeCreateSubnet(vs, utx, txID, signedBytes, creds)
	case *unsigned.ExportTx:
		return e.executeExport(vs, utx, txID, creds)
	case *unsigned.ImportTx:
		return e.executeImport(vs, utx, txID, creds)
	default:
		return nil, fmt.Errorf("expected decision tx but got %T", utx)
	}
}

func (e *executor) ExecuteAtomicTx(
	stx *signed.Tx,
	vs state.Versioned,
) (func() error, error) {
	var (
		txID  = stx.ID()
		creds = stx.Creds
	)

	switch utx := stx.Unsigned.(type) {
	case *unsigned.ExportTx:
		return e.executeExport(vs, utx, txID, creds)
	case *unsigned.ImportTx:
		return e.executeImport(vs, utx, txID, creds)
	default:
		return nil, fmt.Errorf("expected decision tx but got %T", utx)
	}
}

func (e *executor) executeCreateChain(
	vs state.Versioned,
	utx *unsigned.CreateChainTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (func() error, error) {
	// Make sure this transaction is well formed.
	if len(creds) == 0 {
		return nil, unsigned.ErrWrongNumberOfCredentials
	}

	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, err
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(creds) - 1
	baseTxCreds := creds[:baseTxCredsLen]
	subnetCred := creds[baseTxCredsLen]

	// Verify the flowcheck
	createBlockchainTxFee := builder.GetCreateBlockchainTxFee(*e.cfg, vs.GetTimestamp())
	if err := e.spendHandler.SemanticVerifySpend(
		vs,
		utx,
		utx.Ins,
		utx.Outs,
		baseTxCreds,
		createBlockchainTxFee,
		e.ctx.AVAXAssetID,
	); err != nil {
		return nil, err
	}

	subnetIntf, _, err := vs.GetTx(utx.SubnetID)
	if err == database.ErrNotFound {
		return nil, fmt.Errorf("%s isn't a known subnet", utx.SubnetID)
	}
	if err != nil {
		return nil, err
	}

	subnet, ok := subnetIntf.Unsigned.(*unsigned.CreateSubnetTx)
	if !ok {
		return nil, fmt.Errorf("%s isn't a subnet", utx.SubnetID)
	}

	// Verify that this chain is authorized by the subnet
	if err := e.fx.VerifyPermission(utx, utx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return nil, err
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, e.ctx.AVAXAssetID, utx.Outs)

	// Attempt to the new chain to the database
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	vs.AddChain(stx)

	// If this proposal is committed and this node is a member of the
	// subnet that validates the blockchain, create the blockchain
	onAccept := func() error { return p_utils.CreateChain(*e.cfg, utx, txID) }
	return onAccept, nil
}

func (e *executor) executeCreateSubnet(
	vs state.Versioned,
	utx *unsigned.CreateSubnetTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (func() error, error) {
	// Make sure this transaction is well formed.
	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, err
	}

	// Verify the flowcheck
	createSubnetTxFee := builder.GetCreateSubnetTxFee(*e.cfg, vs.GetTimestamp())
	if err := e.spendHandler.SemanticVerifySpend(
		vs,
		utx,
		utx.Ins,
		utx.Outs,
		creds,
		createSubnetTxFee,
		e.ctx.AVAXAssetID,
	); err != nil {
		return nil, err
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, e.ctx.AVAXAssetID, utx.Outs)

	// Attempt to the new chain to the database
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	vs.AddSubnet(stx)

	return nil, nil
}

func (e *executor) executeExport(
	vs state.Versioned,
	utx *unsigned.ExportTx,
	txID ids.ID,
	creds []verify.Verifiable,
) (func() error, error) {
	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, err
	}

	outs := make([]*avax.TransferableOutput, len(utx.Outs)+len(utx.ExportedOutputs))
	copy(outs, utx.Outs)
	copy(outs[len(utx.Outs):], utx.ExportedOutputs)

	if e.bootstrapped.GetValue() {
		if err := verify.SameSubnet(e.ctx, utx.DestinationChain); err != nil {
			return nil, err
		}
	}

	// Verify the flowcheck
	if err := e.spendHandler.SemanticVerifySpend(
		vs,
		utx,
		utx.Ins,
		outs,
		creds,
		e.cfg.TxFee,
		e.ctx.AVAXAssetID,
	); err != nil {
		return nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, e.ctx.AVAXAssetID, utx.Outs)
	return nil, nil
}

func (e *executor) executeImport(
	vs state.Versioned,
	utx *unsigned.ImportTx,
	txID ids.ID,
	creds []verify.Verifiable,
) (func() error, error) {
	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, err
	}

	utxosList := make([]*avax.UTXO, len(utx.Ins)+len(utx.ImportedInputs))
	for index, input := range utx.Ins {
		utxo, err := vs.GetUTXO(input.InputID())
		if err != nil {
			return nil, fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err)
		}
		utxosList[index] = utxo
	}

	if e.bootstrapped.GetValue() {
		if err := verify.SameSubnet(e.ctx, utx.SourceChain); err != nil {
			return nil, err
		}

		utxoIDs := make([][]byte, len(utx.ImportedInputs))
		for i, in := range utx.ImportedInputs {
			utxoID := in.UTXOID.InputID()
			utxoIDs[i] = utxoID[:]
		}
		allUTXOBytes, err := e.ctx.SharedMemory.Get(utx.SourceChain, utxoIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get shared memory: %w", err)
		}

		for i, utxoBytes := range allUTXOBytes {
			utxo := &avax.UTXO{}
			if _, err := unsigned.Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return nil, fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxosList[i+len(utx.Ins)] = utxo
		}

		ins := make([]*avax.TransferableInput, len(utx.Ins)+len(utx.ImportedInputs))
		copy(ins, utx.Ins)
		copy(ins[len(utx.Ins):], utx.ImportedInputs)

		if err := e.spendHandler.SemanticVerifySpendUTXOs(
			utx,
			utxosList,
			ins,
			utx.Outs,
			creds,
			e.cfg.TxFee,
			e.ctx.AVAXAssetID,
		); err != nil {
			return nil, err
		}
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, e.ctx.AVAXAssetID, utx.Outs)
	return nil, nil
}
