// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import "github.com/ava-labs/avalanchego/ids"

var _ Unsigned = (*Transfer)(nil)

type Transfer struct {
	// ChainID provides cross chain replay protection
	ChainID ids.ID `serialize:"true" json:"chainID"`
	// Nonce provides internal chain replay protection
	Nonce   uint64      `serialize:"true" json:"nonce"`
	MaxFee  uint64      `serialize:"true" json:"maxFee"`
	AssetID ids.ID      `serialize:"true" json:"assetID"`
	Amount  uint64      `serialize:"true" json:"amount"`
	To      ids.ShortID `serialize:"true" json:"to"`
}

func (t *Transfer) Visit(v Visitor) error {
	return v.Transfer(t)
}
