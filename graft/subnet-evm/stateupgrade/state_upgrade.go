// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateupgrade

import (
	"bytes"
	"maps"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/evm/utils"

	avalancheutils "github.com/ava-labs/avalanchego/utils"
)

// Block describes all state upgrades to be applied atomically.
type Block struct {
	BlockTimestamp *uint64 `json:"blockTimestamp,omitempty"`

	Accounts map[common.Address]Account `json:"accounts"`
}

// Account describes the modifications to be made to an account during
// a state upgrade.
type Account struct {
	Code          hexutil.Bytes               `json:"code,omitempty"`
	Storage       map[common.Hash]common.Hash `json:"storage,omitempty"`
	BalanceChange *math.HexOrDecimal256       `json:"balanceChange,omitempty"`
}

// Equal reports whether b and other describe the same state modifications.
func (b *Block) Equal(other *Block) bool {
	if b == nil || other == nil {
		return b == other
	}
	return avalancheutils.PtrEqual(b.BlockTimestamp, other.BlockTimestamp) &&
		maps.EqualFunc(b.Accounts, other.Accounts, Account.equal)
}

func (s Account) equal(other Account) bool {
	return bytes.Equal(s.Code, other.Code) &&
		maps.Equal(s.Storage, other.Storage) &&
		utils.BigEqual((*big.Int)(s.BalanceChange), (*big.Int)(other.BalanceChange))
}

// Apply applies the state upgrade to the state.
func (b *Block) Apply(chainConfig *params.ChainConfig, state *state.StateDB, blockNum *big.Int) error {
	isEIP158 := chainConfig.IsEIP158(blockNum)
	for account, upgrade := range b.Accounts {
		if err := upgradeAccount(account, upgrade, state, isEIP158); err != nil {
			return err
		}
	}
	return nil
}

// upgradeAccount applies the state upgrade to the given account.
func upgradeAccount(addr common.Address, acct Account, state *state.StateDB, isEIP158 bool) error {
	// Create the account if it does not exist
	if !state.Exist(addr) {
		state.CreateAccount(addr)
	}

	if acct.BalanceChange != nil {
		balanceChange, _ := uint256.FromBig((*big.Int)(acct.BalanceChange))
		state.AddBalance(addr, balanceChange)
	}
	if len(acct.Code) != 0 {
		// if the nonce is 0, set the nonce to 1 as we would when deploying a contract at
		// the address.
		if isEIP158 && state.GetNonce(addr) == 0 {
			state.SetNonce(addr, 1)
		}
		state.SetCode(addr, acct.Code)
	}
	for key, value := range acct.Storage {
		state.SetState(addr, key, value)
	}
	return nil
}
