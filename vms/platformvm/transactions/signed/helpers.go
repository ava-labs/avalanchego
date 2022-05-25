// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signed

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

// some helper structures useful for storing transactions

type ValidatorAndID struct {
	UnsignedAddValidatorTx *unsigned.AddValidatorTx
	TxID                   ids.ID
}

type SubnetValidatorAndID struct {
	UnsignedAddSubnetValidator *unsigned.AddSubnetValidatorTx
	TxID                       ids.ID
}

type DelegatorAndID struct {
	UnsignedAddDelegatorTx *unsigned.AddDelegatorTx
	TxID                   ids.ID
}
