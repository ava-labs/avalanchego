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

const (
	// MinProposerWindowDuration / MaxProposerWindowDuration bound an explicitly
	// configured ProposerWindowDuration. A value of 0 means "use the default"
	// and is always allowed.
	//
	// The floor is 1s because the proposerVM block timestamp is whole-second
	// granular: a sub-second window gives no real failover benefit (the stall
	// quantizes up to the next second) while shrinking the verify tolerance
	// toward block-propagation time. The ceiling equals the default window
	// (proposer.DefaultWindowDuration); a larger window only slows failover, so
	// there is no reason to raise it above the historical 5s constant.
	MinProposerWindowDuration = 1 * time.Second
	MaxProposerWindowDuration = 5 * time.Second
)

var (
	errAllowedNodesWhenNotValidatorOnly = errors.New("allowedNodes can only be set when ValidatorOnly is true")
	errNoParametersSet                  = errors.New("consensus config must have either snowball or simplex parameters set")
	ErrTooManyConsensusParameters       = errors.New("only one of consensusParameters, snowParameters, or simplexParameters can be set")
	errInvalidProposerWindowDuration    = errors.New("invalid proposerWindowDuration")
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
	// chains in this Subnet. Lowering it shortens the stall when a scheduled
	// proposer is offline (faster failover for CFT/PoA L1s), at the cost of more
	// rejected blocks. If set to 0, the default of 5s is used; any other value
	// must be within [MinProposerWindowDuration, MaxProposerWindowDuration].
	//
	// As a time.Duration this is decoded from JSON as an integer number of
	// nanoseconds (e.g. 1000000000 for 1s), the same as the other duration
	// fields in a Subnet config (snowParameters.maxItemProcessingTime,
	// simplexParameters.maxNetworkDelay).
	//
	// WARNING: This is a network-wide consensus parameter, not a per-node tuning
	// knob. Every validator of the Subnet's chains MUST use the SAME value; a
	// validator with a different window rejects the network's blocks and
	// silently falls out of consensus. It must be coordinated across all
	// validators at once, so it only suits L1s whose validator set is operated
	// as a unit. The primary network (P/C/X) is unaffected and always uses the
	// default.
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

	// 0 means "use the default"; any explicit value must be in range.
	if d := c.ProposerWindowDuration; d != 0 && (d < MinProposerWindowDuration || d > MaxProposerWindowDuration) {
		return fmt.Errorf("%w: %s is not 0 (default) or within [%s, %s]",
			errInvalidProposerWindowDuration, d, MinProposerWindowDuration, MaxProposerWindowDuration)
	}

	if c.SnowParameters != nil {
		return c.SnowParameters.Verify()
	}
	if c.SimplexParameters != nil {
		return c.SimplexParameters.Verify()
	}

	return errNoParametersSet
}
