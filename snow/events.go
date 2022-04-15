// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"github.com/chain4travel/caminogo/ids"
)

// Acceptor is implemented when a struct is monitoring if a message is accepted
type Acceptor interface {
	Accept(ctx *ConsensusContext, containerID ids.ID, container []byte) error
}

// Rejector is implemented when a struct is monitoring if a message is rejected
type Rejector interface {
	Reject(ctx *ConsensusContext, containerID ids.ID, container []byte) error
}

// Issuer is implemented when a struct is monitoring if a message is issued
type Issuer interface {
	Issue(ctx *ConsensusContext, containerID ids.ID, container []byte) error
}
