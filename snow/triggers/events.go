// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package triggers

import "github.com/ava-labs/avalanche-go/ids"

// Acceptor is implemented when a struct is monitoring if a message is accepted
type Acceptor interface {
	Accept(chainID, containerID ids.ID, container []byte) error
}

// Rejector is implemented when a struct is monitoring if a message is rejected
type Rejector interface {
	Reject(chainID, containerID ids.ID, container []byte) error
}

// Issuer is implemented when a struct is monitoring if a message is issued
type Issuer interface {
	Issue(chainID, containerID ids.ID, container []byte) error
}
