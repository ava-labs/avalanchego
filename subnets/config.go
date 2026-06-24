// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/simplex"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	errAllowedNodesWhenNotValidatorOnly      = errors.New("allowedNodes can only be set when ValidatorOnly is true")
	errNoParametersSet                       = errors.New("consensus config must have either snowball or simplex parameters set")
	ErrTooManyConsensusParameters            = errors.New("only one of consensusParameters, snowParameters, or simplexParameters can be set")
	errProposerWindowDurationNotWholeSeconds = errors.New("proposerWindowDuration must be a non-negative whole number of seconds")
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

	// ProposerWindowDuration is the length of a single proposerVM slot for the
	// chains in this Subnet. Lowering it shortens the stall that occurs when a
	// scheduled proposer is offline (faster failover for CFT/PoA L1s), at the
	// cost of more rejected blocks. If set to 0, the default of 5s is used.
	//
	// Must be a whole number of seconds: the proposerVM block timestamp is
	// currently whole-second granular, so sub-second windows make the builder
	// and verifier disagree about proposer slots. See [Config.ValidParameters].
	//
	// WARNING: This value is a network-wide consensus parameter, not a per-node
	// tuning knob. Every validator of a Subnet's chains MUST be configured with
	// the SAME window duration. A validator using a different value disagrees
	// about which proposer is expected in a given slot, so it rejects the
	// network's blocks (and vice versa) and silently fails to reach consensus
	// with the rest of the network. Because there is no on-chain agreement on
	// this value, it must be coordinated out-of-band and rolled out to every
	// validator at once. It is therefore only appropriate for L1s whose
	// validator set is operated as a unit (CFT/PoA).
	//
	// Note: the primary network (P/C/X chains) is unaffected by this field. It
	// always uses the default window; see getPrimaryNetworkConfig and the fact
	// that the primary network cannot be a tracked Subnet.
	ProposerWindowDuration time.Duration `json:"proposerWindowDuration" yaml:"proposerWindowDuration"`
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

	// The proposerVM block timestamp is currently whole-second granular, so a
	// proposer slot boundary (slot * windowDuration) is only representable when
	// windowDuration is a whole number of seconds. Sub-second windows make the
	// block builder and verifier compute different slots from the rounded
	// timestamp, breaking proposer selection during failover. Require whole
	// seconds (a value of 0 falls back to the 5s default).
	//
	// TODO: Remove this restriction once the proposerVM uses millisecond-
	// precision block timestamps and TimeToSlot is exact at sub-second windows.
	if c.ProposerWindowDuration < 0 || c.ProposerWindowDuration%time.Second != 0 {
		return fmt.Errorf("%w: %s", errProposerWindowDurationNotWholeSeconds, c.ProposerWindowDuration)
	}

	if c.SnowParameters != nil {
		return c.SnowParameters.Verify()
	}
	if c.SimplexParameters != nil {
		return c.SimplexParameters.Verify()
	}

	return errNoParametersSet
}
