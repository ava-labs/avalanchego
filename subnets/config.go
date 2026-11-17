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
	errNoParametersSet                  = errors.New("consensus config must have either snowball or simplex parameters set")
	ErrTooManyConsensusParameters       = errors.New("only one of consensusParameters, snowParameters, or simplexParameters can be set")
)

type Config struct {
	// ValidatorOnly indicates that this Subnet's Chains are available to only subnet validators.
	// No chain related messages will go out to non-validators.
	// Validators will drop messages received from non-validators.
	// Also see [AllowedNodes] to allow non-validators to connect to this Subnet.
	ValidatorOnly bool `json:"validatorOnly" yaml:"validatorOnly"`
	// AllowedNodes is the set of node IDs that are explicitly allowed to connect to this Subnet when
	// ValidatorOnly is enabled.
	//
	// Deprecated: Use [MembershipCAPath] or [MembershipCA], which admit a node
	// without editing every other node's config. AllowedNodes is still honoured.
	AllowedNodes set.Set[ids.NodeID] `json:"allowedNodes" yaml:"allowedNodes"`

	// MembershipCAPath is the path to a PEM file holding one or more root
	// certificates. A peer whose staking certificate chain verifies against one
	// of them is a member of this Subnet, whether or not it validates it.
	// Unlike [AllowedNodes], admitting a member edits no other node's config.
	// Nothing in a certificate names a Subnet, so the roots must be
	// this Subnet's alone. At most one of MembershipCAPath and MembershipCA may be set.
	MembershipCAPath string `json:"membershipCAPath" yaml:"membershipCAPath"`
	// MembershipCA inlines the same root certificates as PEM text.
	MembershipCA []string `json:"membershipCA" yaml:"membershipCA"`
	// MembershipCRLPath is the path to a PEM file of revocation lists issued
	// under the membership CA. A certificate they revoke grants no membership, which evicts a member before its
	// certificate expires. It requires a membership CA. At most one of
	// MembershipCRLPath and MembershipCRL may be set.
	MembershipCRLPath string `json:"membershipCRLPath" yaml:"membershipCRLPath"`
	// MembershipCRL inlines the same revocation lists as PEM text.
	MembershipCRL []string `json:"membershipCRL" yaml:"membershipCRL"`

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
	if err := c.validMembership(); err != nil {
		return err
	}

	if c.SnowParameters != nil {
		return c.SnowParameters.Verify()
	}
	if c.SimplexParameters != nil {
		return c.SimplexParameters.Verify()
	}

	return errNoParametersSet
}
