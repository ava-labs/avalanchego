// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// ACP181 implements the epoch logic specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/181-p-chain-epoched-views/README.md
package acp181

import (
	"time"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

// NewEpoch returns a child block's epoch based on its parent.
func NewEpoch(
	upgrades upgrade.Config,
	parentPChainHeight uint64,
	parentEpoch block.Epoch,
	parentTimestamp time.Time,
	childTimestamp time.Time,
) block.Epoch {
	if !upgrades.IsGraniteActivated(childTimestamp) {
		return block.Epoch{}
	}

	if parentEpoch == (block.Epoch{}) {
		// If the parent was not assigned an epoch, then the child is the first
		// block of the initial epoch.
		return block.Epoch{
			PChainHeight: parentPChainHeight,
			Number:       1,
			StartTime:    parentTimestamp.Unix(),
		}
	}

	epochEndTime := time.Unix(parentEpoch.StartTime, 0).Add(upgrades.GraniteEpochDuration)
	if parentTimestamp.Before(epochEndTime) {
		// If the parent was issued before the end of its epoch, then it did not
		// seal the epoch.
		return parentEpoch
	}

	// The parent sealed the epoch, so the child is the first block of the new
	// epoch.
	return block.Epoch{
		PChainHeight: parentPChainHeight,
		Number:       parentEpoch.Number + 1,
		StartTime:    parentTimestamp.Unix(),
	}
}
