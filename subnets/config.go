// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/set"
)

var errAllowedNodesWhenNotValidatorOnly = errors.New("allowedNodes can only be set when ValidatorOnly is true")

type Config struct {
	// ValidatorOnly indicates that this Subnet's Chains are available to only subnet validators.
	// No chain related messages will go out to non-validators.
	// Validators will drop messages received from non-validators.
	// Also see [AllowedNodes] to allow non-validators to connect to this Subnet.
	ValidatorOnly bool `json:"validatorOnly" yaml:"validatorOnly"`
	// AllowedNodes is the set of node IDs that are explicitly allowed to connect to this Subnet when
	// ValidatorOnly is enabled.
	AllowedNodes        set.Set[ids.NodeID] `json:"allowedNodes"        yaml:"allowedNodes"`
	ConsensusParameters snowball.Parameters `json:"consensusParameters" yaml:"consensusParameters"`

	// ProposerMinBlockDelay is the minimum delay this node will enforce when
	// building a snowman++ block.
	//
	// TODO: Remove this flag once all VMs throttle their own block production.
	ProposerMinBlockDelay time.Duration `json:"proposerMinBlockDelay" yaml:"proposerMinBlockDelay"`
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
	if err := c.ConsensusParameters.Verify(); err != nil {
		return fmt.Errorf("consensus %w", err)
	}
	if !c.ValidatorOnly && c.AllowedNodes.Len() > 0 {
		return errAllowedNodesWhenNotValidatorOnly
	}
	return nil
}
