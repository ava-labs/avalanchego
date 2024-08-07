// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var _ verify.Verifiable = (*TimedUTXO)(nil)

type TimedUTXO struct {
	UTXO      `serialize:"true"`
	Timestamp uint64 `serialize:"true" json:"timestamp"`
}

func (utxo *TimedUTXO) Verify() error {
	return utxo.UTXO.Verify()
}
