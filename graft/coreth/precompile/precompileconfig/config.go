// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Defines the stateless interface for unmarshalling an arbitrary config of a precompile
package precompileconfig

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// StatefulPrecompileConfig defines the interface for a stateful precompile to
// be enabled via a network upgrade.
type Config interface {
	// Key returns the unique key for the stateful precompile.
	Key() string
	// Timestamp returns the timestamp at which this stateful precompile should be enabled.
	// 1) 0 indicates that the precompile should be enabled from genesis.
	// 2) n indicates that the precompile should be enabled in the first block with timestamp >= [n].
	// 3) nil indicates that the precompile is never enabled.
	Timestamp() *uint64
	// IsDisabled returns true if this network upgrade should disable the precompile.
	IsDisabled() bool
	// Equal returns true if the provided argument configures the same precompile with the same parameters.
	Equal(Config) bool
	// Verify is called on startup and an error is treated as fatal. Configure can assume the Config has passed verification.
	Verify(ChainConfig) error
}

// PredicateContext is the context passed in to the Predicater interface to verify
// a precompile predicate within a specific ProposerVM wrapper.
type PredicateContext struct {
	SnowCtx *snow.Context
	// ProposerVMBlockCtx defines the ProposerVM context the predicate is verified within
	ProposerVMBlockCtx *block.Context
}

// Predicater is an optional interface for StatefulPrecompileContracts to implement.
// If implemented, the predicate will be called for each predicate included in the
// access list of a transaction.
// PredicateGas will be called while calculating the IntrinsicGas of a transaction
// causing it to be dropped if the total gas goes above the tx gas limit.
// VerifyPredicate is used to populate a bit set of predicates verified prior to
// block execution, which can be accessed via the StateDB during execution.
// The bitset is stored in the block, so that historical blocks can be re-verified
// without calling VerifyPredicate.
type Predicater interface {
	PredicateGas(pred predicate.Predicate, rules Rules) (uint64, error)
	VerifyPredicate(predicateContext *PredicateContext, pred predicate.Predicate) error
}

type WarpMessageWriter interface {
	Add(unsignedMessage *warp.UnsignedMessage) error
}

// AcceptContext defines the context passed in to a precompileconfig's Accepter
type AcceptContext struct {
	SnowCtx *snow.Context
	Warp    WarpMessageWriter
}

// Accepter is an optional interface for StatefulPrecompiledContracts to implement.
// If implemented, Accept will be called for every log with the address of the precompile when the block is accepted.
// WARNING: If you are implementing a custom precompile, beware that coreth
// will not maintain backwards compatibility of this interface and your code should not
// rely on this. Designed for use only by precompiles that ship with coreth.
type Accepter interface {
	Accept(acceptCtx *AcceptContext, blockHash common.Hash, blockNumber uint64, txHash common.Hash, logIndex int, topics []common.Hash, logData []byte) error
}

// ChainContext defines an interface that provides information to a stateful precompile
// about the chain configuration. The precompile can access this information to initialize
// its state.
type ChainConfig interface {
	// IsDurango returns true if the time is after Durango.
	IsDurango(time uint64) bool
}

// Rules defines the interface that provides information about the current rules of the chain.
type Rules interface {
	IsGraniteActivated() bool
}
