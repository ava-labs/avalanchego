// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package hook defines points in an SAE block's lifecycle at which common or
// user-injected behaviour needs to be performed. Functions in this package
// SHOULD be called by all code dealing with a block at the respective point in
// its lifecycle, be that during validation, execution, or otherwise.
package hook

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/intmath"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// PointsG define user-injected hook points.
//
// Directly using this interface as a [BlockBuilder] is indicative of this node
// locally building a block. Calling [PointsG.BlockRebuilderFrom] with an
// existing block is indicative of this node reconstructing a block built
// elsewhere during verification.
type PointsG[T Transaction] interface {
	Points

	BlockBuilder[T]
	// BlockRebuilderFrom returns a [BlockBuilder] that will attempt to
	// reconstruct the provided block. If the provided block is valid for
	// inclusion, then the returned builder MUST be able to reconstruct an
	// identical block.
	BlockRebuilderFrom(block *types.Block) (BlockBuilder[T], error)
}

// Points define user-injected hook points which do not depend on generic
// types.
type Points interface {
	// ExecutionResultsDB opens and returns a height-indexed database, which
	// will be closed by the VM when no longer needed. It MAY use the provided
	// directory for persistence and MUST NOT write data outside of it.
	ExecutionResultsDB(dataDir string) (saetypes.ExecutionResults, error)
	// GasConfigAfter returns the gas target and configuration that should go
	// into effect immediately after the provided block.
	GasConfigAfter(*types.Header) (target gas.Gas, c gastime.GasPriceConfig)
	// BlockTime returns the exact block time for the given header, as recorded
	// in [BlockBuilder.BuildHeader]. The returned time MUST match the header
	// ([time.Time.Unix] == [types.Header.Time]) and MAY include a sub-second
	// component.
	BlockTime(h *types.Header) time.Time
	// Settled returns the extra information for the settled block of the
	// provided header. It MUST match the value passed to
	// [BlockBuilder.BuildBlock].
	Settled(*types.Header) Settled
	// EndOfBlockOps returns operations outside of the normal EVM state changes
	// to perform while executing the block, after regular EVM transactions.
	// These operations will be performed during both worst-case and actual
	// execution.
	EndOfBlockOps(*types.Block) ([]Op, error)
	// CanExecuteTransaction mirrors [params.RulesAllowlistHooks.CanExecuteTransaction]
	// so that consumers can use a single concrete type for both SAE and libevm hooks.
	CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error
	// BeforeExecutingBlock is called immediately prior to executing the block.
	BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error
	// AfterExecutingBlock is called immediately after executing the block.
	AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error
}

// BlockBuilder constructs a block given its components.
type BlockBuilder[T Transaction] interface {
	// BuildHeader constructs a header from the parent header.
	//
	// The returned header MUST have [types.Header.ParentHash],
	// [types.Header.Number] and [types.Header.Time] set appropriately.
	// [types.Header.Root], [types.Header.GasLimit], [types.Header.BaseFee], and
	// [types.Header.GasUsed] will be ignored and overwritten. Any other fields
	// MAY be set as desired.
	//
	// SAE always uses this method instead of directly constructing a header, to
	// ensure any libevm header extras are properly populated.
	BuildHeader(parent *types.Header) (*types.Header, error)
	// PotentialEndOfBlockOps returns an iterator of custom transactions that
	// would be valid to include into a block.
	//
	// The header of the block being built, the hash of the last block to
	// settle, and a block source are provided to allow end of block ops to be
	// filtered based on the worst-case queue.
	//
	// SAE will filter any transactions whose [Op] can not be safely applied to
	// the state.
	PotentialEndOfBlockOps(
		ctx context.Context,
		header *types.Header,
		lastSettledBlock common.Hash,
		source saetypes.BlockSource,
	) iter.Seq[T]
	// BuildBlock constructs a block with the given components. The header
	// MAY be modified, but all other arguments are read-only.
	//
	// SAE always uses this method instead of [types.NewBlock], to ensure any
	// libevm block extras are properly populated.
	// All fields of [SettledState] MUST be populated, otherwise state sync
	// and recovery will not function correctly.
	BuildBlock(
		header *types.Header,
		blockCtx *block.Context,
		txs []*types.Transaction,
		receipts []*types.Receipt,
		endOfBlockOps []T,
		settledStuff Settled,
	) (*types.Block, error)
}

// Transaction is a user-defined transaction type that can be represented as an
// [Op].
type Transaction interface {
	AsOp() Op
}

// AccountDebit includes an amount that an account should have debited,
// along with the nonce used to authorize the debit.
type AccountDebit struct {
	Nonce uint64
	// Amount to deduct from the account balance.
	Amount uint256.Int
	// MinBalance is the minimum balance the account must have for the operation
	// to be valid. It MUST be at least [AccountDebit.Amount].
	MinBalance uint256.Int
}

var errMinBalanceBelowAmount = errors.New("minimum balance below amount to debit")

// Op is an operation that can be applied to state during the execution of a
// block.
type Op struct {
	// ID of this operation. It is used for logging and debugging purposes.
	ID ids.ID
	// Gas consumed by this operation.
	Gas gas.Gas
	// GasFeeCap is the maximum gas price this operation is willing to pay.
	GasFeeCap uint256.Int
	// Burn specifies the amount to decrease account balances by and the nonce
	// used to authorize the debit.
	Burn map[common.Address]AccountDebit
	// Mint specifies the amount to increase account balances by. These funds
	// are not necessarily tied to the funds consumed in the Burn field. The
	// sum of the Mint amounts may exceed the sum of the Burn amounts.
	Mint map[common.Address]uint256.Int
}

// ApplyTo applies the operation to the statedb.
//
// If an account has insufficient funds, [core.ErrInsufficientFunds] is
// returned and the statedb is unchanged.
func (o *Op) ApplyTo(stateDB *state.StateDB) error {
	for from, acc := range o.Burn {
		if acc.MinBalance.Lt(&acc.Amount) {
			return fmt.Errorf("%w: account %s minimum balance %v < amount to debit %v", errMinBalanceBelowAmount, from, acc.MinBalance, acc.Amount)
		}
		if b := stateDB.GetBalance(from); b.Lt(&acc.MinBalance) {
			return core.ErrInsufficientFunds
		}
	}
	for from, acc := range o.Burn {
		// We use the state as the source of truth for the current nonce rather
		// than the value provided by the hook. This prevents any situations,
		// such as with delegated accounts, where nonces might not be
		// incremented properly.
		//
		// If overflow would have occurred here, the nonce must have already
		// been increased by a delegated account's execution, so we are already
		// protected against replay attacks.
		if nonce := stateDB.GetNonce(from); nonce < math.MaxUint64 {
			stateDB.SetNonce(from, nonce+1)
		}
		stateDB.SubBalance(from, &acc.Amount)
	}
	for to, amount := range o.Mint {
		stateDB.AddBalance(to, &amount)
	}
	return nil
}

// MinimumGasConsumption MUST be used as the implementation for the respective
// method on [params.RulesHooks]. The concrete type implementing the hooks MUST
// propagate incoming and return arguments unchanged.
func MinimumGasConsumption(txLimit uint64) uint64 {
	_ = (params.RulesHooks)(nil) // keep the import to allow [] doc links
	return intmath.CeilDiv(txLimit, saeparams.Lambda)
}

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

// Settled includes information about the block that is settled by a header.

type Settled struct {
	Height       uint64  `canoto:"uint,1"`
	GasUnix      uint64  `canoto:"uint,2"`
	GasNumerator gas.Gas `canoto:"uint,3"`
	Excess       gas.Gas `canoto:"uint,4"`

	canotoData canotoData_Settled
}

// SettledGasTime is a helper that given a header and its settler, returns the
// [gastime.Time] associated with the post-execution state of the header.
//
// TODO(alarso16): This should be moved to the state sync logic once implemented.
func SettledGasTime(h Points, settled, settler *types.Header) (*gastime.Time, error) {
	target, cfg := h.GasConfigAfter(settled)
	s := h.Settled(settler)

	pt := proxytime.New(s.GasUnix, s.GasNumerator, gastime.SafeRateOfTarget(target))
	return gastime.FromProxyTime(pt, target, s.Excess, cfg)
}
