// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ currentValidator = &currentValidatorImpl{}

type currentValidator interface {
	validatorIntf

	// return txs.AddValidatorTx content along with
	// the ID of its signed.Tx
	AddValidatorTx() (*txs.AddValidatorTx, ids.ID)

	// Weight of delegations to this validator. Doesn't include the stake
	// provided by this validator.
	DelegatorWeight() uint64

	PotentialReward() uint64
}

type currentValidatorImpl struct {
	// delegators are sorted in order of removal.
	validatorImpl

	addValidator    ValidatorAndID
	delegatorWeight uint64
	potentialReward uint64
}

func (v *currentValidatorImpl) AddValidatorTx() (*txs.AddValidatorTx, ids.ID) {
	return v.addValidator.Tx, v.addValidator.TxID
}

func (v *currentValidatorImpl) DelegatorWeight() uint64 {
	return v.delegatorWeight
}

func (v *currentValidatorImpl) PotentialReward() uint64 {
	return v.potentialReward
}
