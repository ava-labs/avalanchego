// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/utils/set"
)

var errAllowedNodesWhenNotValidatorOnly = errors.New("allowedNodes can only be set when ValidatorOnly is true")

type GossipConfig struct {
	AcceptedFrontierValidatorSize    uint `json:"gossipAcceptedFrontierValidatorSize" yaml:"gossipAcceptedFrontierValidatorSize"`
	AcceptedFrontierNonValidatorSize uint `json:"gossipAcceptedFrontierNonValidatorSize" yaml:"gossipAcceptedFrontierNonValidatorSize"`
	AcceptedFrontierPeerSize         uint `json:"gossipAcceptedFrontierPeerSize" yaml:"gossipAcceptedFrontierPeerSize"`
	OnAcceptValidatorSize            uint `json:"gossipOnAcceptValidatorSize" yaml:"gossipOnAcceptValidatorSize"`
	OnAcceptNonValidatorSize         uint `json:"gossipOnAcceptNonValidatorSize" yaml:"gossipOnAcceptNonValidatorSize"`
	OnAcceptPeerSize                 uint `json:"gossipOnAcceptPeerSize" yaml:"gossipOnAcceptPeerSize"`
	AppGossipValidatorSize           uint `json:"appGossipValidatorSize" yaml:"appGossipValidatorSize"`
	AppGossipNonValidatorSize        uint `json:"appGossipNonValidatorSize" yaml:"appGossipNonValidatorSize"`
	AppGossipPeerSize                uint `json:"appGossipPeerSize" yaml:"appGossipPeerSize"`
}

type Config struct {
	GossipConfig

	// ValidatorOnly indicates that this Subnet's Chains are available to only subnet validators.
	// No chain related messages will go out to non-validators.
	// Validators will drop messages received from non-validators.
	// Also see [AllowedNodes] to allow non-validators to connect to this Subnet.
	ValidatorOnly bool `json:"validatorOnly" yaml:"validatorOnly"`
	// AllowedNodes is the set of node IDs that are explicitly allowed to connect to this Subnet when
	// ValidatorOnly is enabled.
	AllowedNodes        set.Set[ids.NodeID]  `json:"allowedNodes" yaml:"allowedNodes"`
	ConsensusParameters avalanche.Parameters `json:"consensusParameters" yaml:"consensusParameters"`

	// ProposerMinBlockDelay is the minimum delay this node will enforce when
	// building a snowman++ block.
	// TODO: Remove this flag once all VMs throttle their own block production.
	ProposerMinBlockDelay time.Duration `json:"proposerMinBlockDelay" yaml:"proposerMinBlockDelay"`

	// See comment on [MinPercentConnectedStakeHealthy] in platformvm.Config
	MinPercentConnectedStakeHealthy float64 `json:"minPercentConnectedStakeHealthy" yaml:"minPercentConnectedStakeHealthy"`
}

func (c *Config) Valid() error {
	if err := c.ConsensusParameters.Valid(); err != nil {
		return fmt.Errorf("consensus parameters are invalid: %w", err)
	}
	if !c.ValidatorOnly && c.AllowedNodes.Len() > 0 {
		return errAllowedNodesWhenNotValidatorOnly
	}
	return nil
}
