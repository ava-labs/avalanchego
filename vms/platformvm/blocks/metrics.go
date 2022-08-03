// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

type Metrics interface {
	MarkAcceptedOptionVote()
	MarkRejectedOptionVote()
	MarkAccepted(b Block) error
}
