// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// FromReceipts returns the outbound messages included in receipts.
func FromReceipts(rs types.Receipts) ([]*avalanchewarp.UnsignedMessage, error) {
	var messages []*avalanchewarp.UnsignedMessage
	for _, r := range rs {
		for _, log := range r.Logs {
			if log.Address != corethwarp.ContractAddress {
				continue
			}

			m, err := corethwarp.UnpackSendWarpEventDataToMessage(log.Data)
			if err != nil {
				return nil, fmt.Errorf("parsing log data into warp message (TxHash: %s, LogIndex: %d): %w", log.TxHash, log.Index, err)
			}
			messages = append(messages, m)
		}
	}
	return messages, nil
}
