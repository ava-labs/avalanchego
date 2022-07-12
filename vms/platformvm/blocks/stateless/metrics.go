// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

type Metrics interface {
	MarkVoteWon()
	MarkVoteLost()
	MarkAccepted(b Block) error
}
