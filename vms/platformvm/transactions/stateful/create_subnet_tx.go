// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ DecisionTx = &CreateSubnetTx{}

type CreateSubnetTx struct {
	*unsigned.CreateSubnetTx

	txID        ids.ID // ID of signed create subnet tx
	signedBytes []byte // signed Tx bytes, needed to recreate signed.Tx
	creds       []verify.Verifiable

	verifier TxVerifier
}

// InputUTXOs for [DecisionTxs] will return an empty set to diffrentiate from the [AtomicTxs] input UTXOs
func (tx *CreateSubnetTx) InputUTXOs() ids.Set { return nil }

func (tx *CreateSubnetTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	return ids.ID{}, nil, nil
}
