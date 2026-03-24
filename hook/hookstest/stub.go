// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package hookstest provides a test double for SAE's [hook] package.
package hookstest

import (
	"iter"
	"math/big"
	"slices"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"

	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saetest"
	saetypes "github.com/ava-labs/strevm/types"
)

// Stub implements [hook.PointsG] parameterized by [Op].
type Stub struct {
	Now                     func() time.Time
	Target                  gas.Gas
	Ops                     []Op
	ExecutionResultsDBFn    func(string) (saedb.ExecutionResults, error)
	CanExecuteTransactionFn func(common.Address, *common.Address, libevm.StateReader) error
	GasPriceConfig          hook.GasPriceConfig
}

var _ hook.PointsG[Op] = (*Stub)(nil)

// HookOption applies a configuration to [Stub].
type HookOption = options.Option[Stub]

// WithGasPriceConfig overrides the default gas config.
func WithGasPriceConfig(cfg hook.GasPriceConfig) HookOption {
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

// WithOps overrides the default end-of-block ops.
func WithOps(ops []Op) HookOption {
	return options.Func[Stub](func(s *Stub) {
		s.Ops = ops
	})
}

// WithExecutionResultsDBFn overrides the default ExecutionResultsDB function.
func WithExecutionResultsDBFn(fn func(string) (saedb.ExecutionResults, error)) HookOption {
	return options.Func[Stub](func(s *Stub) {
		s.ExecutionResultsDBFn = fn
	})
}

// NewStub returns a stub with defaults applied.
// It uses [gastime.DefaultGasPriceConfig] unless overridden by [WithGasPriceConfig].
func NewStub(target gas.Gas, opts ...HookOption) *Stub {
	// defaultGasPriceConfig is the same as [gastime.DefaultGasPriceConfig]. It is defined
	// here to avoid a circular dependency between [gastime] and [hookstest].
	defaultGasPriceConfig := hook.GasPriceConfig{
		TargetToExcessScaling: 87,
		MinPrice:              1,
		StaticPricing:         false,
	}
	return options.ApplyTo(&Stub{
		Target:         target,
		GasPriceConfig: defaultGasPriceConfig,
	}, opts...)
}

// ExecutionResultsDB propagates arguments to and from
// [Stub.ExecutionResultsDBFn] if non-nil, otherwise it returns a fresh
// [saetest.NewHeightIndexDB] on every call.
func (s *Stub) ExecutionResultsDB(dataDir string) (saedb.ExecutionResults, error) {
	if fn := s.ExecutionResultsDBFn; fn != nil {
		return fn(dataDir)
	}
	return saedb.ExecutionResults{
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
		Time:       uint64(now.Unix()), //nolint:gosec // Known non-negative
		Extra:      e.MarshalCanoto(),
	}
	return hdr, nil
}

// PotentialEndOfBlockOps ignores its arguments and returns [Stub.Ops] as a
// sequence.
func (s *Stub) PotentialEndOfBlockOps(header *types.Header, lastSettledBlock common.Hash, source saetypes.BlockSource) iter.Seq[Op] {
	return slices.Values(s.Ops)
}

// BuildBlock calls [BuildBlock] with its arguments.
func (*Stub) BuildBlock(
	header *types.Header,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	ops []Op,
) (*types.Block, error) {
	return BuildBlock(header, txs, receipts, ops)
}

// BuildBlock encodes ops into [types.Header.Extra] and calls [types.NewBlock]
// with the other arguments.
func BuildBlock(
	header *types.Header,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	ops []Op,
) (*types.Block, error) {
	var e extra
	// If the header originally had fractional seconds set, we keep them in the
	// built block.
	if err := e.UnmarshalCanoto(header.Extra); err != nil {
		return nil, err
	}

	e.ops = ops
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

	return NewStub(s.Target, WithOps(e.ops), WithNow(func() time.Time {
		return time.Unix(
			int64(b.Time()), //nolint:gosec // Won't overflow for a few millennia
			int64(e.subSec),
		)
	})), nil
}

// GasConfigAfter ignores its argument and always returns [Stub.Target] and [Stub.GasPriceConfig].
func (s *Stub) GasConfigAfter(*types.Header) (gas.Gas, hook.GasPriceConfig) {
	return s.Target, s.GasPriceConfig
}

// SubSecondBlockTime returns the sub-second time encoded and stored by
// [Stub.BuildHeader] in the header's `Extra` field. If said field is empty,
// SubSecondBlockTime returns 0.
func (s *Stub) SubSecondBlockTime(hdr *types.Header) time.Duration {
	var e extra
	if err := e.UnmarshalCanoto(hdr.Extra); err != nil {
		// This is left as a panic to avoid polluting various functions with
		// error returns when no error is possible in production.
		panic(err)
	}
	return e.subSec
}

// EndOfBlockOps return the ops included in the block by [BuildBlock].
func (s *Stub) EndOfBlockOps(b *types.Block) ([]hook.Op, error) {
	var e extra
	if err := e.UnmarshalCanoto(b.Extra()); err != nil {
		return nil, err
	}
	ops := make([]hook.Op, len(e.ops))
	for i, op := range e.ops {
		ops[i] = op.AsOp()
	}
	return ops, nil
}

// CanExecuteTransaction proxies to [Stub.CanExecuteTransactionFn] if non-nil,
// otherwise it allows all transactions.
func (s *Stub) CanExecuteTransaction(from common.Address, to *common.Address, sr libevm.StateReader) error {
	if fn := s.CanExecuteTransactionFn; fn != nil {
		return fn(from, to, sr)
	}
	return nil
}

// BeforeExecutingBlock is a no-op that always returns nil.
func (*Stub) BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error {
	return nil
}

// AfterExecutingBlock is a no-op that always returns nil.
func (*Stub) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error {
	return nil
}

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

type extra struct {
	subSec time.Duration `canoto:"int,1"` //nolint:staticcheck // subSec intentionally communicates that the value is < time.Second
	ops    []Op          `canoto:"repeated value,2"`

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
