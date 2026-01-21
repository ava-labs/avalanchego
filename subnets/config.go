// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/simplex"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	errAllowedNodesWhenNotValidatorOnly = errors.New("allowedNodes can only be set when ValidatorOnly is true")
	errInvalidConsensusConfiguration    = errors.New("consensus config must have either snowball or simplex parameters set")
	ErrUnsupportedConsensusParameters   = errors.New("consensusParameters is deprecated; use either snowballParameters or simplexParameters instead")
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

	SnowParameters    *snowball.Parameters `json:"snowballParameters" yaml:"snowballParameters"`
	SimplexParameters *simplex.Parameters  `json:"simplexParameters"  yaml:"simplexParameters"`

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
}

func (c *Config) Valid() error {
	if err := c.validateConsensusParameters(); err != nil {
		return err
	}

	if !c.ValidatorOnly && c.AllowedNodes.Len() > 0 {
		return errAllowedNodesWhenNotValidatorOnly
	}
	return nil
}

func (c *Config) validateConsensusParameters() error {
	if c.SnowParameters != nil && c.SimplexParameters == nil {
		return c.SnowParameters.Verify()
	}

	if c.SimplexParameters != nil && c.SnowParameters == nil {
		return c.SimplexParameters.Verify()
	}

	// We still allow the deprecated consensus parameters to be set and verified, but will remove this in the future.
	if c.ConsensusParameters != nil && c.SnowParameters == nil && c.SimplexParameters == nil {
		c.SnowParameters = c.ConsensusParameters
		return c.SnowParameters.Verify()
	}

	return errInvalidConsensusConfiguration
}
