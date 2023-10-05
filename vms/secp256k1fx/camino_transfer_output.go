// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"encoding/json"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	_ verify.State       = (*CrossTransferOutput)(nil)
	_ TransferOutputIntf = (*TransferOutput)(nil)

	ErrEmptyRecipient = errors.New("receipient cannot be empty")
)

type TransferOutputIntf interface {
	verify.Verifiable
	Amount() uint64
	Owners() interface{}
}

// Used in a cross transfer, this output can be used to specify
// the recipient on the target chain at export time
type CrossTransferOutput struct {
	TransferOutput `serialize:"true"`

	// The recipient address
	Recipient ids.ShortID `serialize:"true" json:"recipient"`
}

func (out *CrossTransferOutput) MarshalJSON() ([]byte, error) {
	result, err := out.OutputOwners.Fields()
	if err != nil {
		return nil, err
	}

	result["amount"] = out.Amt
	result["recipient"] = out.Recipient
	return json.Marshal(result)
}

func (out *CrossTransferOutput) Verify() error {
	if err := out.TransferOutput.Verify(); err != nil {
		return err
	}

	if out.Recipient == ids.ShortEmpty {
		return ErrEmptyRecipient
	}

	return nil
}

// Used in vms/platformvm/txs/executor/camino_tx_executor.go func outputsAreEqual
func (out *TransferOutput) Equal(to any) bool {
	toOut, ok := to.(*TransferOutput)
	return ok && out.Amt == toOut.Amt && out.OutputOwners.Equals(&toOut.OutputOwners)
}
