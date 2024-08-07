// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

type UTXOWithMSig struct {
	UTXO    `serialize:"true"`
	Aliases []verify.State `serialize:"true" json:"aliases"`
}

func (utxo *UTXOWithMSig) Verify() error {
	for _, alias := range utxo.Aliases {
		if err := alias.Verify(); err != nil {
			return err
		}
	}

	return utxo.UTXO.Verify()
}
