// (c) 2023 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
)

// StateUpgrade describes the modifications to be made to the state during
// a state upgrade.
type StateUpgrade struct {
	BlockTimestamp *big.Int `json:"blockTimestamp,omitempty"`

	// map from account address to the modification to be made to the account.
	StateUpgradeAccounts map[common.Address]StateUpgradeAccount `json:"accounts"`
}

// StateUpgradeAccount describes the modifications to be made to an account during
// a state upgrade.
type StateUpgradeAccount struct {
	Code          hexutil.Bytes               `json:"code,omitempty"`
	Storage       map[common.Hash]common.Hash `json:"storage,omitempty"`
	BalanceChange *math.HexOrDecimal256       `json:"balanceChange,omitempty"`
}

func (s *StateUpgrade) Equal(other *StateUpgrade) bool {
	return reflect.DeepEqual(s, other)
}
