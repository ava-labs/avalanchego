// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/common"
)

var errZeroAmount = errors.New("zero amount")

type EVMInput struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
	Nonce   uint64         `serialize:"true" json:"nonce"`
}

func (e EVMInput) Compare(o EVMInput) int {
	if c := e.Address.Cmp(o.Address); c != 0 {
		return c
	}
	return e.AssetID.Compare(o.AssetID)
}

func (e EVMInput) Verify() error {
	if e.Amount == 0 {
		return errZeroAmount
	}
	return nil
}

type EVMOutput struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
}

func (e EVMOutput) Compare(o EVMOutput) int {
	if c := e.Address.Cmp(o.Address); c != 0 {
		return c
	}
	return e.AssetID.Compare(o.AssetID)
}

func (e EVMOutput) Verify() error {
	if e.Amount == 0 {
		return errZeroAmount
	}
	return nil
}
