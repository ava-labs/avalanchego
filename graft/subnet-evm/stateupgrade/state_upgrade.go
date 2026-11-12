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
	Timestamp *uint64 `json:"blockTimestamp,omitempty"`

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
// It MUST treat a config as equal to its own JSON round-trip, since the copy
// persisted by WriteChainConfig is compared against a fresh parse of the
// upgrade bytes on every startup.
func (b *Block) Equal(other *Block) bool {
	if b == nil || other == nil {
		return b == other
	}
	return avalancheutils.PtrEqual(b.Timestamp, other.Timestamp) &&
		maps.EqualFunc(b.Accounts, other.Accounts, Account.equal)
}

func (a Account) equal(other Account) bool {
	return bytes.Equal(a.Code, other.Code) &&
		maps.Equal(a.Storage, other.Storage) &&
		utils.BigEqual((*big.Int)(a.BalanceChange), (*big.Int)(other.BalanceChange))
}

// Apply applies the state upgrade to the state.
func (b *Block) Apply(chainConfig *params.ChainConfig, state *state.StateDB, blockNum *big.Int) {
	isEIP158 := chainConfig.IsEIP158(blockNum)
	for account, upgrade := range b.Accounts {
		upgradeAccount(account, upgrade, state, isEIP158)
	}
}

// upgradeAccount applies the state upgrade to the given account.
func upgradeAccount(addr common.Address, acct Account, stateDB *state.StateDB, isEIP158 bool) {
	// Create the account if it does not exist
	if !stateDB.Exist(addr) {
		stateDB.CreateAccount(addr)
	}

	if acct.BalanceChange != nil {
		balanceChange, _ := uint256.FromBig((*big.Int)(acct.BalanceChange))
		stateDB.AddBalance(addr, balanceChange)
	}
	if len(acct.Code) != 0 {
		// if the nonce is 0, set the nonce to 1 as we would when deploying a contract at
		// the address.
		if isEIP158 && stateDB.GetNonce(addr) == 0 {
			stateDB.SetNonce(addr, 1)
		}
		stateDB.SetCode(addr, acct.Code)
	}
	for key, value := range acct.Storage {
		stateDB.SetState(addr, key, value)
	}
}
