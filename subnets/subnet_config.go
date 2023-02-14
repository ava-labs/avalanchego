// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"time"

	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

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
	ValidatorOnly       bool                 `json:"validatorOnly" yaml:"validatorOnly"`
	ConsensusParameters avalanche.Parameters `json:"consensusParameters" yaml:"consensusParameters"`

	// ProposerMinBlockDelay is the minimum delay this node will enforce when
	// building a snowman++ block.
	// TODO: Remove this flag once all VMs throttle their own block production.
	ProposerMinBlockDelay time.Duration `json:"proposerMinBlockDelay" yaml:"proposerMinBlockDelay"`

	// See comment on [MinPercentConnectedStakeHealthy] in platformvm.Config
	MinPercentConnectedStakeHealthy float64 `json:"minPercentConnectedStakeHealthy"`
}
