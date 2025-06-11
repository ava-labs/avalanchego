// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type ValidatorInfo interface {
	// GetValidatorSet returns the validators of the provided subnet at the
	// requested P-chain height.
	// The returned map should not be modified.
	GetValidatorSet(
		ctx context.Context,
		height uint64,
		subnetID ids.ID,
	) (map[ids.NodeID]*validators.GetValidatorOutput, error)
}

// Config wraps all the parameters needed for a simplex engine
type Config struct {
	Ctx        SimplexChainContext
	Log        logging.Logger
	Validators ValidatorInfo
	SignBLS    SignFunc
}

// Context is information about the current execution.
// [SubnetID] is the ID of the subnet this context exists within.
// [ChainID] is the ID of the chain this context exists within.
// [NodeID] is the ID of this node
type SimplexChainContext struct {
	NodeID   ids.NodeID
	ChainID  ids.ID
	SubnetID ids.ID
}
