// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
)

func (cs *caminoState) SetLastRewardImportTimestamp(timestamp uint64) {
	cs.caminoDiff.newRewardImportTimestamp = &timestamp
}

func (cs *caminoState) writeValidatorRewards() error {
	if cs.newRewardImportTimestamp != nil {
		if err := database.PutUInt64(
			cs.caminoDB,
			lastRewardImportTimestampKey,
			*cs.newRewardImportTimestamp,
		); err != nil {
			return fmt.Errorf("failed to write rewardImportTimestamp: %w", err)
		}
		cs.newRewardImportTimestamp = nil
	}
	return nil
}
