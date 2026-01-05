// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

var _ Unsigned = (*Import)(nil)

type Import struct {
	// Nonce provides internal chain replay protection
	Nonce  uint64 `serialize:"true" json:"nonce"`
	MaxFee uint64 `serialize:"true" json:"maxFee"`
	// Message includes the chainIDs to provide cross chain replay protection
	Message []byte `serialize:"true" json:"message"`
}

func (i *Import) Visit(v Visitor) error {
	return v.Import(i)
}
