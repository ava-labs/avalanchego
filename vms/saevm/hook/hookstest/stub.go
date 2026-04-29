// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package hookstest provides a test double for SAE's [hook] package.
package hookstest

import (
	"context"
	"iter"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// Stub implements [hook.PointsG] parameterized by [Op].
type Stub struct {
	Now                     func() time.Time
	Target                  gas.Gas
	InvalidOpIDs            set.Set[ids.ID]
	Ops                     []Op
	ExecutionResultsDBFn    func(string) (saetypes.ExecutionResults, error)
	CanExecuteTransactionFn func(params.Rules, common.Address, *common.Address, libevm.StateReader) error
	BeforeExecutingBlockFn  func(params.Rules, *types.Header, *state.StateDB, *types.Block) error
	GasPriceConfig          gastime.GasPriceConfig
}

var _ hook.PointsG[Op] = (*Stub)(nil)

// HookOption applies a configuration to [Stub].
type HookOption = options.Option[Stub]

// WithGasPriceConfig overrides the default gas config.
func WithGasPriceConfig(cfg gastime.GasPriceConfig) HookOption {
	return options.Func[Stub](func(s *Stub) {
		s.GasPriceConfig = cfg
	})
}

// WithNow overrides the default time source.
func WithNow(now func() time.Time) HookOption {
	return options.Func[Stub](func(s *Stub) {
		s.Now = now
	})
}

// WithInvalidOpIDs overrides the default invalid end-of-block opIDs.
func WithInvalidOpIDs(invalidOps set.Set[ids.ID]) HookOption {
	return options.Func[Stub](func(s *Stub) {
		s.InvalidOpIDs = invalidOps
	})
}

// WithOps overrides the default end-of-block ops.
func WithOps(ops []Op) HookOption {
	return options.Func[Stub](func(s *Stub) {
		s.Ops = ops
	})
}

// WithExecutionResultsDBFn overrides the default ExecutionResultsDB function.
func WithExecutionResultsDBFn(fn func(string) (saetypes.ExecutionResults, error)) HookOption {
	return options.Func[Stub](func(s *Stub) {
		s.ExecutionResultsDBFn = fn
	})
}

// NewStub returns a stub with defaults applied.
// It uses [gastime.DefaultGasPriceConfig] unless overridden by [WithGasPriceConfig].
func NewStub(target gas.Gas, opts ...HookOption) *Stub {
	return options.ApplyTo(&Stub{
		Target:         target,
		GasPriceConfig: gastime.DefaultGasPriceConfig(),
	}, opts...)
}

// ExecutionResultsDB propagates arguments to and from
// [Stub.ExecutionResultsDBFn] if non-nil, otherwise it returns a fresh
// [saetest.NewHeightIndexDB] on every call.
func (s *Stub) ExecutionResultsDB(dataDir string) (saetypes.ExecutionResults, error) {
	if fn := s.ExecutionResultsDBFn; fn != nil {
		return fn(dataDir)
	}
	return saetypes.ExecutionResults{
		HeightIndex: saetest.NewHeightIndexDB(),
	}, nil
}

// BuildHeader constructs a header that builds on top of the parent header. The
// `Extra` field SHOULD NOT be modified as it encodes the sub-second block time
// and end-of-block ops.
func (s *Stub) BuildHeader(parent *types.Header) (*types.Header, error) {
	var now time.Time
	if s.Now != nil {
		now = s.Now()
	} else {
		now = time.Now()
	}

	e := extra{
		subSec: time.Duration(now.Nanosecond()),
	}
	hdr := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		Time:       uint64(now.Unix()), //#nosec G115 -- Known non-negative
		Extra:      e.MarshalCanoto(),
	}
	return hdr, nil
}

// PotentialEndOfBlockOps ignores its arguments and returns [Stub.Ops] as a
// sequence.
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (s *Stub) PotentialEndOfBlockOps(ctx context.Context, header *types.Header, lastSettledBlock common.Hash, source saetypes.BlockSource) iter.Seq[Op] {
	return func(yield func(Op) bool) {
		for _, op := range s.Ops {
			if s.InvalidOpIDs.Contains(op.ID) {
				continue
			}
			if !yield(op) {
				return
			}
		}
	}
}

// BuildBlock calls [BuildBlock] with its arguments.
func (*Stub) BuildBlock(
	header *types.Header,
	blockCtx *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	ops []Op,
	settledHeight uint64,
) (*types.Block, error) {
	return BuildBlock(header, blockCtx, txs, receipts, ops, settledHeight)
}

// BuildBlock encodes ops into [types.Header.Extra] and calls [types.NewBlock]
// with the other arguments.
func BuildBlock(
	header *types.Header,
	_ *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	ops []Op,
	settledHeight uint64,
) (*types.Block, error) {
	var e extra
	// If the header originally had fractional seconds set, we keep them in the
	// built block.
	if err := e.UnmarshalCanoto(header.Extra); err != nil {
		return nil, err
	}

	e.ops = ops
	e.settledHeight = settledHeight
	header.Extra = e.MarshalCanoto()
	return types.NewBlock(header, txs, nil, receipts, saetest.TrieHasher()), nil
}

// BlockRebuilderFrom returns a block builder that uses the provided block as a
// source of time.
func (s *Stub) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[Op], error) {
	var e extra
	if err := e.UnmarshalCanoto(b.Extra()); err != nil {
		return nil, err
	}

	return NewStub(s.Target, WithInvalidOpIDs(s.InvalidOpIDs), WithOps(e.ops), WithNow(func() time.Time {
		return time.Unix(
			int64(b.Time()), //#nosec G115 -- Won't overflow for a few millennia
			int64(e.subSec),
		)
	})), nil
}

// GasConfigAfter ignores its argument and always returns [Stub.Target] and [Stub.GasPriceConfig].
func (s *Stub) GasConfigAfter(*types.Header) (gas.Gas, gastime.GasPriceConfig) {
	return s.Target, s.GasPriceConfig
}

// BlockTime returns exact block time from [Stub.BuildHeader] by combining the
// stored seconds in [types.Header.Time] and the sub-second component from
// [types.Header.Extra].
func (*Stub) BlockTime(hdr *types.Header) time.Time {
	subSec := getHeaderExtra(hdr).subSec             //nolint:staticcheck // subSec intentionally communicates that the value is < time.Second
	return time.Unix(int64(hdr.Time), int64(subSec)) //#nosec G115 -- Won't overflow for a few millennia
}

// SettledHeight returns the height encoded in the Header by [Stub.BuildBlock]
// or [BuildBlock].
func (*Stub) SettledHeight(hdr *types.Header) uint64 {
	return getHeaderExtra(hdr).settledHeight
}

// EndOfBlockOps return the ops included in the block by [BuildBlock].
func (*Stub) EndOfBlockOps(b *types.Block) ([]hook.Op, error) {
	eOps := getHeaderExtra(b.Header()).ops
	hookOps := make([]hook.Op, len(eOps))
	for i, op := range eOps {
		hookOps[i] = op.AsOp()
	}
	return hookOps, nil
}

func getHeaderExtra(hdr *types.Header) extra {
	var e extra
	if err := e.UnmarshalCanoto(hdr.Extra); err != nil {
		// This is left as a panic to avoid polluting various functions with
		// error returns when no error is possible in production.
		panic(err)
	}
	return e
}

// CanExecuteTransaction proxies to [Stub.CanExecuteTransactionFn] if non-nil,
// otherwise it allows all transactions.
func (s *Stub) CanExecuteTransaction(rules params.Rules, from common.Address, to *common.Address, sr libevm.StateReader) error {
	if fn := s.CanExecuteTransactionFn; fn != nil {
		return fn(rules, from, to, sr)
	}
	return nil
}

// BeforeExecutingBlock proxies to [Stub.BeforeExecutingBlockFn] if non-nil,
// otherwise it is a no-op that always returns nil.
func (s *Stub) BeforeExecutingBlock(rules params.Rules, parent *types.Header, sdb *state.StateDB, block *types.Block) error {
	if fn := s.BeforeExecutingBlockFn; fn != nil {
		return fn(rules, parent, sdb, block)
	}
	return nil
}

// AfterExecutingBlock is a no-op that always returns nil.
func (*Stub) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error {
	return nil
}

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

//nolint:revive // struct-tag: canoto allows unexported fields
type extra struct {
	subSec        time.Duration `canoto:"int,1"` //nolint:staticcheck // subSec intentionally communicates that the value is < time.Second
	ops           []Op          `canoto:"repeated value,2"`
	settledHeight uint64        `canoto:"uint,3"`

	canotoData canotoData_extra
}

// Op is a serializable representation of [hook.Op].
type Op struct {
	ID        ids.ID          `canoto:"fixed bytes,1"`
	Gas       gas.Gas         `canoto:"uint,2"`
	GasFeeCap uint256.Int     `canoto:"fixed repeated uint,3"`
	Burn      []AccountDebit  `canoto:"repeated value,4"`
	Mint      []AccountCredit `canoto:"repeated value,5"`

	canotoData canotoData_Op
}

// AsOp converts the op into a representation that SAE can use directly.
func (o Op) AsOp() hook.Op {
	hookOp := hook.Op{
		ID:        o.ID,
		Gas:       o.Gas,
		GasFeeCap: o.GasFeeCap,
		Burn:      make(map[common.Address]hook.AccountDebit, len(o.Burn)),
		Mint:      make(map[common.Address]uint256.Int, len(o.Mint)),
	}
	for _, b := range o.Burn {
		hookOp.Burn[b.Address] = hook.AccountDebit{
			Nonce:      b.Nonce,
			Amount:     b.Amount,
			MinBalance: b.MinBalance,
		}
	}
	for _, m := range o.Mint {
		hookOp.Mint[m.Address] = m.Amount
	}
	return hookOp
}

// AccountDebit is a serializable representation of an entry in [hook.Op.Burn].
type AccountDebit struct {
	Address    common.Address `canoto:"fixed bytes,1"`
	Nonce      uint64         `canoto:"uint,2"`
	Amount     uint256.Int    `canoto:"fixed repeated uint,3"`
	MinBalance uint256.Int    `canoto:"fixed repeated uint,4"`

	canotoData canotoData_AccountDebit
}

// AccountCredit is a serializable representation of an entry in [hook.Op.Mint].
type AccountCredit struct {
	Address common.Address `canoto:"fixed bytes,1"`
	Amount  uint256.Int    `canoto:"fixed repeated uint,2"`

	canotoData canotoData_AccountCredit
}
