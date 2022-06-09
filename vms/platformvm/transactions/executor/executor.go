// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
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
	ProposalExecutor
	DecisionExecutor
	AtomicExecutor

	// Attempts to verify this transaction with the provided txstate.
	SemanticVerify(
		stx *signed.Tx,
		parentState state.Mutable,
	) error
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

	pe := proposalExecutor{components: components}
	de := decisionExecutor{components: components}
	ae := atomicExecutor{decisionExecutor: &de}

	return &executor{
		proposalExecutor: pe,
		decisionExecutor: de,
		atomicExecutor:   ae,
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
	decisionExecutor
	atomicExecutor
}

// Attempts to verify this transaction with the provided txstate.
func (e *executor) SemanticVerify(
	stx *signed.Tx,
	parentState state.Mutable,
) error {
	switch utx := stx.Unsigned.(type) {
	case *unsigned.AddDelegatorTx,
		*unsigned.AddValidatorTx,
		*unsigned.AddSubnetValidatorTx,
		*unsigned.AdvanceTimeTx,
		*unsigned.RewardValidatorTx:
		return e.semanticVerifyProposal(stx, parentState)

	case *unsigned.CreateChainTx,
		*unsigned.CreateSubnetTx:
		return e.semanticVerifyDecision(stx, parentState)

	case *unsigned.ExportTx,
		*unsigned.ImportTx:
		return e.semanticVerifyAtomic(stx, parentState)

	default:
		return fmt.Errorf("tx type %T could not be semantically verified", utx)
	}
}
