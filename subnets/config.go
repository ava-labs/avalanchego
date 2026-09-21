// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/simplex"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	errAllowedNodesWhenNotValidatorOnly  = errors.New("allowedNodes can only be set when ValidatorOnly is true")
	ErrLargeMessagesWhenNotValidatorOnly = errors.New("largeMessages can only be set when ValidatorOnly is true")
	ErrMemberCAWhenNotValidatorOnly      = errors.New("memberCAPath and memberCA can only be set when ValidatorOnly is true")
	errNoParametersSet                   = errors.New("consensus config must have either snowball or simplex parameters set")
	ErrTooManyConsensusParameters        = errors.New("only one of consensusParameters, snowParameters, or simplexParameters can be set")
	ErrTooManyMemberCASources            = errors.New("only one of memberCAPath or memberCA can be set")
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
	// It marks members by node ID, complementing [MemberCAPath] / [MemberCAPEMs],
	// which mark them by certificate. Adding a node this way means editing every
	// other node's config, so a CA scales better for a fleet, but both are
	// supported and may be used side by side.
	AllowedNodes set.Set[ids.NodeID] `json:"allowedNodes" yaml:"allowedNodes"`

	// MemberCAPath is the path to a PEM file holding one or more root
	// certificates. A peer whose staking certificate chain verifies against one
	// of them is a member of this Subnet, whether or not it validates it.
	//
	// Exactly one of MemberCAPath and MemberCAPEMs may be set.
	MemberCAPath string `json:"memberCAPath" yaml:"memberCAPath"`

	// MemberCAPEMs inlines the same root certificates as PEM text, for
	// deployments that would rather not manage a second file.
	MemberCAPEMs []string `json:"memberCA" yaml:"memberCA"`

	// LargeMessages, when set, declares that members of this Subnet exchange
	// P2P frames larger than the default and fixes their size. The node builds
	// a single elevated stack, so at most one tracked Subnet may set it.
	LargeMessages *LargeMessagesConfig `json:"largeMessages" yaml:"largeMessages"`

	// memberCA is the parsed form of MemberCAPath / MemberCAPEMs, populated by
	// [Config.LoadMemberCA]. It is nil when this Subnet has no member CA.
	memberCA *MemberCA

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

// MaxAncestorsBytes returns the cumulative byte budget for an Ancestors
// response sent by a chain in this Subnet. For the default frame that is
// [constants.MaxContainersLen].
func (c *Config) MaxAncestorsBytes() int {
	if c.LargeMessages == nil {
		return constants.MaxContainersLen
	}
	// Four fifths of the frame: the ratio [constants.MaxContainersLen] applies
	// to the default frame, kept so that an elevated frame fills the same way.
	// The budget counts only the raw container bytes that GetAncestors
	// accumulates; the remaining fifth is headroom for what wraps them on the
	// wire - the Ancestors message envelope, a length prefix per container,
	// and any growth from compressing bytes that are already compressed - so
	// that a full response still fits the frame. The arithmetic is done in
	// uint64 because 4 * MaxMessageSize overflows uint32 above 1 GiB.
	return int(4 * uint64(c.LargeMessages.MaxMessageSize) / 5)
}

// MemberCA returns the roots that a peer's certificate chain must verify
// against to be a member of this Subnet, or nil if it declares none.
func (c *Config) MemberCA() *MemberCA {
	return c.memberCA
}

// LoadMemberCA parses MemberCAPath or MemberCAPEMs into the CA returned by
// [Config.MemberCA]. It is a no-op when neither is set.
func (c *Config) LoadMemberCA() error {
	switch {
	case c.MemberCAPath != "" && len(c.MemberCAPEMs) > 0:
		return ErrTooManyMemberCASources
	case c.MemberCAPath != "":
		pemBytes, err := os.ReadFile(c.MemberCAPath)
		if err != nil {
			return fmt.Errorf("reading memberCAPath: %w", err)
		}
		c.memberCA, err = ParseMemberCA(pemBytes)
		if err != nil {
			return fmt.Errorf("parsing %q: %w", c.MemberCAPath, err)
		}
	case len(c.MemberCAPEMs) > 0:
		var err error
		c.memberCA, err = ParseMemberCA([]byte(strings.Join(c.MemberCAPEMs, "\n")))
		if err != nil {
			return fmt.Errorf("parsing memberCA: %w", err)
		}
	}
	return nil
}

func (c *Config) validateValidatorOnlyOptions() error {
	if c.ValidatorOnly {
		return nil
	}
	if c.AllowedNodes.Len() > 0 {
		return errAllowedNodesWhenNotValidatorOnly
	}
	if c.LargeMessages != nil {
		return ErrLargeMessagesWhenNotValidatorOnly
	}
	if c.MemberCAPath != "" || len(c.MemberCAPEMs) > 0 {
		return ErrMemberCAWhenNotValidatorOnly
	}
	return nil
}

func (c *Config) ValidParameters() error {
	if err := c.validateValidatorOnlyOptions(); err != nil {
		return err
	}

	if c.LargeMessages != nil {
		if err := c.LargeMessages.Verify(); err != nil {
			return err
		}
	}

	if c.SnowParameters != nil {
		return c.SnowParameters.Verify()
	}
	if c.SimplexParameters != nil {
		return c.SimplexParameters.Verify()
	}

	return errNoParametersSet
}
