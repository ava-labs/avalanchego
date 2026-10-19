// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"
)

// PredicateBytesFromExtra returns the predicate result bytes from the header's
// extra data. If the extra data is not long enough, an empty slice is returned.
func PredicateBytesFromExtra(rules cparams.AvalancheRules, extra []byte) []byte {
	offset := predicateBytesOffset(rules)
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to `offset`.
	// After Durango, the VM pre-verifies the extra data past `offset` is valid.
	if len(extra) <= offset {
		return nil
	}
	return extra[offset:]
}

func predicateBytesOffset(rules cparams.AvalancheRules) int {
	switch {
	case rules.IsHelicon:
		return 0
	case rules.IsFortuna:
		return acp176.StateSize
	default:
		return ap3.WindowSize
	}
}
