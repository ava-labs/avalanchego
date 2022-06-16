// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/ids"
)

// some helper structures useful for storing transactions

type ValidatorAndID struct {
	UnsignedAddValidatorTx *AddValidatorTx
	TxID                   ids.ID
}

type SubnetValidatorAndID struct {
	UnsignedAddSubnetValidator *AddSubnetValidatorTx
	TxID                       ids.ID
}

type DelegatorAndID struct {
	UnsignedAddDelegatorTx *AddDelegatorTx
	TxID                   ids.ID
}
