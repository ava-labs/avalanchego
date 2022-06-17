// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// some helper structures useful for storing transactions

type ValidatorAndID struct {
	Tx   *txs.AddValidatorTx
	TxID ids.ID
}

type SubnetValidatorAndID struct {
	Tx   *txs.AddSubnetValidatorTx
	TxID ids.ID
}

type DelegatorAndID struct {
	Tx   *txs.AddDelegatorTx
	TxID ids.ID
}
