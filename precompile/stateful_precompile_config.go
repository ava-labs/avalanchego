// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// StateDB is the interface for accessing the EVM State.
type StateDB interface {
	GetState(common.Address, common.Hash) common.Hash
	SetState(common.Address, common.Hash, common.Hash)
}

// StatefulPrecompileConfig defines the interface for a stateful precompile to
type StatefulPrecompileConfig interface {
	// PrecompileAddress returns the address of the stateful precompile
	PrecompileAddress() common.Address
	// Timestamp returns the timestamp that the stateful precompile is enabled or nil
	// if it is not enabled.
	Timestamp() *big.Int
	// Configure is called on the first block where the stateful precompile should be enabled.
	// This allows the stateful precompile to configure its own state via [StateDB] as necessary.
	// This function must be deterministic since it will impact the EVM state. If a change to the
	// config causes a change to the state modifications made in Configure, then it cannot be safely
	// made to the config after the network upgrade has gone into effect.
	Configure(StateDB)
}

// CheckConfigure checks if [config] is activated by the transition from block at [parentTimestamp] to [currentTimestamp].
// If it does, then it calls Configure on [config] to make the necessary state update to enable the StatefulPrecompile.
// Note: this function is called within genesis to configure the starting state if it [config] specifies that it should be
// configured at genesis, or happens during block processing to update the state before processing the given block.
// TODO: add ability to call Configure at different timestamps, so that developers can easily re-configure by updating the
// stateful precompile config.
func CheckConfigure(parentTimestamp *big.Int, currentTimestamp *big.Int, config StatefulPrecompileConfig, state StateDB) {
	// If the stateful precompile is nil, skip configuring it
	if config == nil {
		return
	}
	forkTimestamp := config.Timestamp()
	isParentForked := isForked(parentTimestamp, forkTimestamp)
	isCurrentBlockForked := isForked(currentTimestamp, forkTimestamp)
	// If the network upgrade goes into effect within this transition, configure the stateful precompile
	if !isParentForked && isCurrentBlockForked {
		config.Configure(state)
	}
}

// isForked returns whether a fork scheduled at block s is active at the given head block.
func isForked(s, head *big.Int) bool {
	if s == nil || head == nil {
		return false
	}
	return s.Cmp(head) <= 0
}
