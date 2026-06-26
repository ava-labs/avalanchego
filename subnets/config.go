// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/simplex"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	// Bounds for an explicitly configured ProposerWindowMilliseconds; 0 means
	// "use the default". The floor is 50ms: roughly the smallest window in which
	// a proposer can build and propagate a block before its slot closes. A
	// sub-second window only actually advances the slot clock when the chain also
	// sets ProposerMillisecondTimestamps (this PR reinterprets the proposerVM
	// block timestamp as unix-milliseconds); without it, whole-second timestamps
	// quantize the slot clock to ~1s and a sub-second window gains nothing. The
	// ceiling is the default window, since a larger window only slows failover.
	MinProposerWindowMilliseconds = 50
	MaxProposerWindowMilliseconds = 5_000
)

var (
	errAllowedNodesWhenNotValidatorOnly  = errors.New("allowedNodes can only be set when ValidatorOnly is true")
	errNoParametersSet                   = errors.New("consensus config must have either snowball or simplex parameters set")
	ErrTooManyConsensusParameters        = errors.New("only one of consensusParameters, snowParameters, or simplexParameters can be set")
	errInvalidProposerWindowMilliseconds = errors.New("invalid proposerWindowMilliseconds")
)

type Config struct {
	// ValidatorOnly indicates that this Subnet's Chains are available to only subnet validators.
	// No chain related messages will go out to non-validators.
	// Validators will drop messages received from non-validators.
	// Also see [AllowedNodes] to allow non-validators to connect to this Subnet.
	ValidatorOnly bool `json:"validatorOnly" yaml:"validatorOnly"`
	// AllowedNodes is the set of node IDs that are explicitly allowed to connect to this Subnet when
	// ValidatorOnly is enabled.
	AllowedNodes set.Set[ids.NodeID] `json:"allowedNodes" yaml:"allowedNodes"`

	// Deprecated: Use either SnowParameters or SimplexParameters instead.
	ConsensusParameters *snowball.Parameters `json:"consensusParameters" yaml:"consensusParameters"`

	SnowParameters    *snowball.Parameters `json:"snowParameters"    yaml:"snowParameters"`
	SimplexParameters *simplex.Parameters  `json:"simplexParameters" yaml:"simplexParameters"`

	// ProposerNumHistoricalBlocks is the number of historical snowman++ blocks
	// this node will index per chain. If set to 0, the node will index all
	// snowman++ blocks.
	//
	// Note: The last accepted block is not considered a historical block. This
	// prevents the user from only storing the last accepted block, which can
	// never be safe due to the non-atomic commits between the proposervm
	// database and the innerVM's database.
	//
	// Invariant: This value must be set such that the proposervm never needs to
	// rollback more blocks than have been deleted. On startup, the proposervm
	// rolls back its accepted chain to match the innerVM's accepted chain. If
	// the innerVM is not persisting its last accepted block quickly enough, the
	// database can become corrupted.
	//
	// TODO: Move this flag once the proposervm is configurable on a per-chain
	// basis.
	ProposerNumHistoricalBlocks uint64 `json:"proposerNumHistoricalBlocks" yaml:"proposerNumHistoricalBlocks"`

	// ProposerWindowMilliseconds is the length of a single proposerVM slot for
	// the chains in this Subnet, in milliseconds. Lowering it shortens the stall
	// when a scheduled proposer is offline (faster failover for CFT/PoA L1s) at
	// the cost of more rejected blocks. 0 uses the default (5s); other values
	// must be within [MinProposerWindowMilliseconds, MaxProposerWindowMilliseconds].
	//
	// Invariant: this is a network-wide consensus parameter, not a per-node
	// knob. Every validator of the Subnet's chains must use the same value or
	// the Subnet loses liveness. The primary network (P/C/X) is unaffected.
	ProposerWindowMilliseconds uint64 `json:"proposerWindowMilliseconds" yaml:"proposerWindowMilliseconds"`

	// ProposerMillisecondTimestamps interprets the proposerVM wrapper block's
	// timestamp as unix-milliseconds instead of unix-seconds, so that a
	// sub-second ProposerWindowMilliseconds can actually advance (with
	// whole-second timestamps the slot clock only ticks once per second, so a
	// sub-second window gains nothing). It is opt-in and only meaningful
	// alongside a sub-second proposer window.
	//
	// WARNING: this is a Subnet-wide consensus parameter, not a per-node knob.
	// Every validator of the Subnet's chains MUST use the SAME value, and it MUST
	// be fixed from genesis: enabling it on a chain that already has whole-second
	// history misreads every old block. The primary network (P/C/X) is unaffected
	// and always uses whole-second timestamps.
	ProposerMillisecondTimestamps bool `json:"proposerMillisecondTimestamps" yaml:"proposerMillisecondTimestamps"`
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ValidConsensusConfiguration ensures that at most one consensus parameter type is set.
// If none are set, then the default snowball parameters will be used for SnowParameters.
func (c *Config) ValidConsensusConfiguration() error {
	numSet := boolToInt(c.SimplexParameters != nil) +
		boolToInt(c.SnowParameters != nil) +
		boolToInt(c.ConsensusParameters != nil)
	if numSet > 1 {
		return ErrTooManyConsensusParameters
	}
	return nil
}

func (c *Config) ValidParameters() error {
	if !c.ValidatorOnly && c.AllowedNodes.Len() > 0 {
		return errAllowedNodesWhenNotValidatorOnly
	}

	if ms := c.ProposerWindowMilliseconds; ms != 0 && (ms < MinProposerWindowMilliseconds || ms > MaxProposerWindowMilliseconds) {
		return fmt.Errorf("%w: %dms is not 0 (default) or within [%dms, %dms]",
			errInvalidProposerWindowMilliseconds, ms, MinProposerWindowMilliseconds, MaxProposerWindowMilliseconds)
	}

	if c.SnowParameters != nil {
		return c.SnowParameters.Verify()
	}
	if c.SimplexParameters != nil {
		return c.SimplexParameters.Verify()
	}

	return errNoParametersSet
}
