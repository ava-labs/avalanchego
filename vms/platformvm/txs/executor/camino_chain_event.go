// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

// GetNextChainEventTime returns the next chain event time
// For example: stakers set changed, deposit expired
func GetNextChainEventTime(_ state.Chain, stakerChangeTime time.Time) (time.Time, error) {
	return stakerChangeTime, nil
}
