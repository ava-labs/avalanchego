// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import "github.com/ava-labs/avalanchego/ids"

var _ Unsigned = (*Export)(nil)

type Export struct {
	// ChainID provides cross chain replay protection
	ChainID ids.ID `serialize:"true" json:"chainID"`
	// Nonce provides internal chain replay protection
	Nonce       uint64      `serialize:"true" json:"nonce"`
	MaxFee      uint64      `serialize:"true" json:"maxFee"`
	PeerChainID ids.ID      `serialize:"true" json:"peerChainID"`
	IsReturn    bool        `serialize:"true" json:"isReturn"`
	Amount      uint64      `serialize:"true" json:"amount"`
	To          ids.ShortID `serialize:"true" json:"to"`
}

func (e *Export) Visit(v Visitor) error {
	return v.Export(e)
}
