// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateupgrade

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
)

// Configure applies the state upgrade to the state.
// Note: This function should not return errors as any error would break the chain.
// All validation should be done in verifyStateUpgrades at config load time.
func Configure(stateUpgrade *extras.StateUpgrade, chainConfig ChainContext, state StateDB, blockContext BlockContext) error {
	isEIP158 := chainConfig.IsEIP158(blockContext.Number())
	for account, upgrade := range stateUpgrade.StateUpgradeAccounts {
		upgradeAccount(account, upgrade, state, isEIP158)
	}
	return nil
}

// upgradeAccount applies the state upgrade to the given account.
func upgradeAccount(account common.Address, upgrade extras.StateUpgradeAccount, state StateDB, isEIP158 bool) {
	// Create the account if it does not exist
	if !state.Exist(account) {
		state.CreateAccount(account)
	}

	// Balance change detected - update the balance of the account
	if upgrade.BalanceChange != nil {
		// BalanceChange is a HexOrDecimal256, which is just a typed big.Int
		// Note: We validate at config load time that the change itself fits in uint256,
		// but we still need to check if the resulting balance would overflow/underflow.
		bigChange := (*big.Int)(upgrade.BalanceChange)
		currentBalance := state.GetBalance(account)

		switch bigChange.Sign() {
		case 1: // Positive - check for overflow
			balanceChange, _ := uint256.FromBig(bigChange)
			_, overflow := new(uint256.Int).AddOverflow(currentBalance, balanceChange)
			if overflow {
				log.Warn("State upgrade balance change would overflow, clamping to max uint256",
					"account", account.Hex(),
					"currentBalance", currentBalance.ToBig().String(),
					"balanceChange", balanceChange.ToBig().String())
				state.SubBalance(account, currentBalance)
				state.AddBalance(account, new(uint256.Int).SetAllOne())
			} else {
				state.AddBalance(account, balanceChange)
			}
		case -1: // Negative - check for underflow
			absChange := new(big.Int).Abs(bigChange)
			balanceChange, _ := uint256.FromBig(absChange)
			if currentBalance.Cmp(balanceChange) < 0 {
				log.Warn("State upgrade balance change would underflow, clamping to zero",
					"account", account.Hex(),
					"currentBalance", currentBalance.ToBig().String(),
					"balanceChange", balanceChange.ToBig().String())
				state.SubBalance(account, currentBalance)
			} else {
				state.SubBalance(account, balanceChange)
			}
		}
		// If zero (Sign() == 0), do nothing
	}

	if len(upgrade.Code) != 0 {
		// if the nonce is 0, set the nonce to 1 as we would when deploying a contract at
		// the address.
		if isEIP158 && state.GetNonce(account) == 0 {
			state.SetNonce(account, 1)
		}
		state.SetCode(account, upgrade.Code)
	}
	for key, value := range upgrade.Storage {
		state.SetState(account, key, value)
	}
}
