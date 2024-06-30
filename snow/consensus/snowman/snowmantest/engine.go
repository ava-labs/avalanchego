// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowmantest

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

func MakeLastAcceptedBlockF(blks ...[]*Block) func(context.Context) (ids.ID, error) {
	return func(_ context.Context) (ids.ID, error) {
		var (
			highestHeight uint64
			highestID     ids.ID
		)
		for _, blks := range blks {
			for _, blk := range blks {
				if blk.Status() == choices.Accepted && blk.Height() >= highestHeight {
					highestHeight = blk.Height()
					highestID = blk.ID()
				}
			}
		}
		return highestID, nil
	}
}
