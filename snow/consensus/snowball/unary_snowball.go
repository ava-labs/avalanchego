// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
)

var _ UnarySnowball = &unarySnowball{}

// unarySnowball is the implementation of a unary snowball instance
type unarySnowball struct {
	// wrap the unary snowflake logic
	unarySnowflake

	// numSuccessfulPolls tracks the total number of successful network polls
	numSuccessfulPolls int
}

func (sb *unarySnowball) RecordSuccessfulPoll() {
	sb.numSuccessfulPolls++
	sb.unarySnowflake.RecordSuccessfulPoll()
}

func (sb *unarySnowball) Extend(beta int, choice int) BinarySnowball {
	bs := &binarySnowball{
		binarySnowflake: binarySnowflake{
			binarySlush: binarySlush{preference: choice},
			confidence:  sb.confidence,
			beta:        beta,
			finalized:   sb.Finalized(),
		},
		preference: choice,
	}
	bs.numSuccessfulPolls[choice] = sb.numSuccessfulPolls
	return bs
}

func (sb *unarySnowball) Clone() UnarySnowball {
	newSnowball := *sb
	return &newSnowball
}

func (sb *unarySnowball) String() string {
	return fmt.Sprintf("SB(NumSuccessfulPolls = %d, %s)",
		sb.numSuccessfulPolls,
		&sb.unarySnowflake)
}
