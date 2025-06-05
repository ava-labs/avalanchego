// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type ValidatorInfo interface {
	GetValidatorIDs(subnetID ids.ID) []ids.NodeID
	GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*validators.Validator, bool)
}

// Config wraps all the parameters needed for a simplex engine
type Config struct {
	Ctx        SimplexChainContext
	Log        logging.Logger
	Validators ValidatorInfo
	SignBLS    SignFunc
}

// Context is information about the current execution.
// [SubnitID] is the ID of the subnet this context exists within.
// [ChainID] is the ID of the chain this context exists within.
// [NodeID] is the ID of this node
type SimplexChainContext struct {
	NodeID   ids.NodeID
	ChainID  ids.ID
	SubnetID ids.ID
}
