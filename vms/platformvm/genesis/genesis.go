// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
)

// UTXO adds messages to UTXOs
type UTXO struct {
	avax.UTXO `serialize:"true"`
	Message   []byte `serialize:"true" json:"message"`
}

// Genesis represents a genesis state of the platform chain
type Genesis struct {
	UTXOs         []*UTXO      `serialize:"true"`
	Validators    []*signed.Tx `serialize:"true"`
	Chains        []*signed.Tx `serialize:"true"`
	Timestamp     uint64       `serialize:"true"`
	InitialSupply uint64       `serialize:"true"`
	Message       string       `serialize:"true"`
}

func New(genesisBytes []byte) (*Genesis, error) {
	gen := &Genesis{}
	if _, err := Codec.Unmarshal(genesisBytes, gen); err != nil {
		return nil, err
	}
	for _, tx := range gen.Validators {
		if err := tx.Sign(Codec, nil); err != nil {
			return nil, err
		}
	}
	for _, tx := range gen.Chains {
		if err := tx.Sign(Codec, nil); err != nil {
			return nil, err
		}
	}
	return gen, nil
}
