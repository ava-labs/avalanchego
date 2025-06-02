// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type ValidatorInfo interface {
	GetValidatorIDs(subnetID ids.ID) []ids.NodeID
	GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*validators.Validator, bool)
}

type ConsensusParams struct {
	MaxProposalWait time.Duration 
	MaxRebroadcastWait  time.Duration
}

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	// Things i've been through
	Ctx        SimplexChainContext
	Validators ValidatorInfo
	VM         block.ChainVM
	SignBLS            func(msg []byte) (*bls.Signature, error)
	GenesisData        []byte


	// things i havne't been through
	GetServer          common.AllGetsServer
	Sender             Sender
	OutboundMsgBuilder message.OutboundMsgBuilder
	DB                 database.Database

}

type SimplexChainContext struct {
	NodeID   ids.NodeID
	ChainID  ids.ID
	SubnetID ids.ID
	Log      logging.Logger

	Params ConsensusParams
}