// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"fmt"
)

// TODO: Consider renaming Message to, say, VMMessage

// Message is an enum of the message types that vms can send to consensus
type Message uint32

const (
	// PendingTxs notifies a consensus engine that
	// its VM has pending transactions
	// (i.e. it would like to add a new block/vertex to consensus)
	PendingTxs Message = iota
)

func (msg Message) String() string {
	switch msg {
	case PendingTxs:
		return "Pending Transactions"
	default:
		return fmt.Sprintf("Unknown Message: %d", msg)
	}
}
