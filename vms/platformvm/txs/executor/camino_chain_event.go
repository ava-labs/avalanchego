// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

// GetNextChainEventTime returns the next chain event time
// (stakers set changed / deposit expired / validator rewards distibution)
func GetNextChainEventTime(state state.Chain, stakerChangeTime time.Time) (time.Time, error) {
	cfg, err := state.Config()
	if err != nil {
		return time.Time{}, fmt.Errorf("couldn't get config: %w", err)
	}

	if cfg.CaminoConfig.ValidatorsRewardPeriod == 0 {
		return stakerChangeTime, nil
	}

	validatorsRewardTime := getNextValidatorsRewardTime(
		uint64(state.GetTimestamp().Unix()),
		cfg.CaminoConfig.ValidatorsRewardPeriod,
	)

	if stakerChangeTime.Before(validatorsRewardTime) {
		return stakerChangeTime, nil
	}

	return validatorsRewardTime, nil
}

func getNextValidatorsRewardTime(chainTime uint64, validatorsRewardPeriod uint64) time.Time {
	period := chainTime / validatorsRewardPeriod
	return time.Unix(int64(period*validatorsRewardPeriod+validatorsRewardPeriod), 0)
}
