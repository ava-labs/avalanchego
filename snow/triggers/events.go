// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package triggers

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

// Acceptor is implemented when a struct is monitoring if a message is accepted
type Acceptor interface {
	Accept(ctx *snow.ConsensusContext, containerID ids.ID, container []byte) error
}

// Rejector is implemented when a struct is monitoring if a message is rejected
type Rejector interface {
	Reject(ctx *snow.ConsensusContext, containerID ids.ID, container []byte) error
}

// Issuer is implemented when a struct is monitoring if a message is issued
type Issuer interface {
	Issue(ctx *snow.ConsensusContext, containerID ids.ID, container []byte) error
}
