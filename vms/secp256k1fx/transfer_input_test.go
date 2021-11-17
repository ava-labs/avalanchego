// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestTransferInputAmount(t *testing.T) {
	in := TransferInput{
		Amt: 1,
		Input: Input{
			SigIndices: []uint32{0, 1},
		},
	}
	if amount := in.Amount(); amount != 1 {
		t.Fatalf("Input.Amount returned the wrong amount. Result: %d ; Expected: %d", amount, 1)
	}
}

func TestTransferInputVerify(t *testing.T) {
	in := TransferInput{
		Amt: 1,
		Input: Input{
			SigIndices: []uint32{0, 1},
		},
	}
	err := in.Verify()
	if err != nil {
		t.Fatal(err)
	}
}

func TestTransferInputVerifyNil(t *testing.T) {
	in := (*TransferInput)(nil)
	err := in.Verify()
	if err == nil {
		t.Fatalf("Should have errored with a nil input")
	}
}

func TestTransferInputVerifyNoValue(t *testing.T) {
	in := TransferInput{
		Amt: 0,
		Input: Input{
			SigIndices: []uint32{0, 1},
		},
	}
	err := in.Verify()
	if err == nil {
		t.Fatalf("Should have errored with a no value input")
	}
}

func TestTransferInputVerifyDuplicated(t *testing.T) {
	in := TransferInput{
		Amt: 1,
		Input: Input{
			SigIndices: []uint32{0, 0},
		},
	}
	err := in.Verify()
	if err == nil {
		t.Fatalf("Should have errored with duplicated indices")
	}
}

func TestTransferInputVerifyUnsorted(t *testing.T) {
	in := TransferInput{
		Amt: 1,
		Input: Input{
			SigIndices: []uint32{1, 0},
		},
	}
	err := in.Verify()
	if err == nil {
		t.Fatalf("Should have errored with unsorted indices")
	}
}

func TestTransferInputSerialize(t *testing.T) {
	c := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	if err := m.RegisterCodec(0, c); err != nil {
		t.Fatal(err)
	}

	expected := []byte{
		// Codec version
		0x00, 0x00,
		// amount:
		0x00, 0x00, 0x00, 0x00, 0x07, 0x5b, 0xcd, 0x15,
		// length:
		0x00, 0x00, 0x00, 0x02,
		// sig[0]
		0x00, 0x00, 0x00, 0x03,
		// sig[1]
		0x00, 0x00, 0x00, 0x07,
	}
	in := TransferInput{
		Amt: 123456789,
		Input: Input{
			SigIndices: []uint32{3, 7},
		},
	}
	err := in.Verify()
	if err != nil {
		t.Fatal(err)
	}

	result, err := m.Marshal(0, &in)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(expected, result) {
		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
	}
}

func TestTransferInputNotState(t *testing.T) {
	intf := interface{}(&TransferInput{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
