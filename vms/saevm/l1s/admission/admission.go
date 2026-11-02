// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admission

import (
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

// TxAllowList rejects `from` if the transaction allowlist is active in the
// settled rules and the sender's settled role is not enabled.
func TxAllowList(from common.Address, settled hook.SettledView) error {
	rules := params.GetRulesExtra(settled.Rules)
	if !rules.IsPrecompileEnabled(txallowlist.ContractAddress) {
		return nil
	}

	role := txallowlist.GetTxAllowListStatus(settled.State, from)
	if role.IsEnabled() {
		return nil
	}
	return fmt.Errorf("%w: %s", vmerrors.ErrSenderAddressNotAllowListed, from)
}
