// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"testing"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/vms/components/verify"
	"github.com/chain4travel/caminogo/vms/secp256k1fx"
)

func TestTransferOutputVerifyNil(t *testing.T) {
	to := (*TransferOutput)(nil)
	if err := to.Verify(); err == nil {
		t.Fatalf("TransferOutput.Verify should have errored on nil")
	}
}

func TestTransferOutputLargePayload(t *testing.T) {
	to := TransferOutput{
		Payload: make([]byte, MaxPayloadSize+1),
	}
	if err := to.Verify(); err == nil {
		t.Fatalf("TransferOutput.Verify should have errored on too large of a payload")
	}
}

func TestTransferOutputInvalidSecp256k1Output(t *testing.T) {
	to := TransferOutput{
		OutputOwners: secp256k1fx.OutputOwners{
			Addrs: []ids.ShortID{
				ids.ShortEmpty,
				ids.ShortEmpty,
			},
		},
	}
	if err := to.Verify(); err == nil {
		t.Fatalf("TransferOutput.Verify should have errored on too large of a payload")
	}
}

func TestTransferOutputState(t *testing.T) {
	intf := interface{}(&TransferOutput{})
	if _, ok := intf.(verify.State); !ok {
		t.Fatalf("should be marked as state")
	}
}
