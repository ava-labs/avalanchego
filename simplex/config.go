// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	GenesisData        []byte
	GetServer          common.AllGetsServer
	SignBLS            func(msg []byte) (*bls.Signature, error)
	Sender             Sender
	OutboundMsgBuilder message.OutboundMsgBuilder
	DB                 database.Database

	Ctx        *snow.ConsensusContext
	VM         block.ChainVM
	Validators validators.Manager
}
