// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateupgrade

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
)

// Configure applies the state upgrade to the state.
func Configure(stateUpgrade *extras.StateUpgrade, chainConfig ChainContext, state StateDB, blockContext BlockContext) error {
	isEIP158 := chainConfig.IsEIP158(blockContext.Number())
	for account, upgrade := range stateUpgrade.StateUpgradeAccounts {
		if err := upgradeAccount(account, upgrade, state, isEIP158); err != nil {
			return err
		}
	}
	return nil
}

// upgradeAccount applies the state upgrade to the given account.
func upgradeAccount(account common.Address, upgrade extras.StateUpgradeAccount, state StateDB, isEIP158 bool) error {
	// Create the account if it does not exist
	if !state.Exist(account) {
		state.CreateAccount(account)
	}

	// Balance change detected - update the balance of the account
	if upgrade.BalanceChange != nil {
		// BalanceChange is a HexOrDecimal256, which is just a typed big.Int
		bigChange := (*big.Int)(upgrade.BalanceChange)
		switch bigChange.Sign() {
		case 1: // Positive
			// ParseBig256 already enforced 256-bit limit during JSON parsing, so no overflow check needed
			balanceChange, _ := uint256.FromBig(bigChange)
			state.AddBalance(account, balanceChange)
		case -1: // Negative
			absChange := new(big.Int).Abs(bigChange)
			balanceChange, _ := uint256.FromBig(absChange)
			currentBalance := state.GetBalance(account)
			if currentBalance.Cmp(balanceChange) < 0 {
				return fmt.Errorf("insufficient balance for subtraction: account %s has %s but trying to subtract %s",
					account.Hex(), currentBalance.ToBig().String(), balanceChange.ToBig().String())
			}
			state.SubBalance(account, balanceChange)
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
	return nil
}
