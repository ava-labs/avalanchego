// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
)

// heldXChainAsset is the state-account extra recording whether the account has
// ever held an X-chain asset.
type heldXChainAsset bool

// HeldXChainAsset reports whether addr has ever held a non-zero balance of an
// X-chain asset. The flag is never cleared, even if the balance returns to zero.
func HeldXChainAsset(s *state.StateDB, addr common.Address) bool {
	return bool(state.GetExtra(s, extras.StateAccount, addr))
}

// MarkHeldXChainAsset records that addr has held a non-zero balance of an
// X-chain asset.
func MarkHeldXChainAsset(s *state.StateDB, addr common.Address) {
	state.SetExtra(s, extras.StateAccount, addr, heldXChainAsset(true))
}
