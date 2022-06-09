// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ DecisionTx = &CreateChainTx{}

const (
	MaxNameLen    = 128
	MaxGenesisLen = units.MiB
)

type CreateChainTx struct {
	*unsigned.CreateChainTx

	txID        ids.ID // ID of signed create chain tx
	signedBytes []byte // signed Tx bytes, needed to recreate signed.Tx
	creds       []verify.Verifiable

	verifier TxVerifier
}

func (tx *CreateChainTx) InputUTXOs() ids.Set { return nil }

func (tx *CreateChainTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	return ids.ID{}, nil, nil
}
