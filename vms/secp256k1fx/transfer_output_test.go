// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestOutputAmount(t *testing.T) {
	out := TransferOutput{
		Amt: 1,
		OutputOwners: OutputOwners{
			Locktime:  1,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.ShortEmpty,
			},
		},
	}
	if amount := out.Amount(); amount != 1 {
		t.Fatalf("Output.Amount returned the wrong amount. Result: %d ; Expected: %d", amount, 1)
	}
}

func TestOutputVerify(t *testing.T) {
	out := TransferOutput{
		Amt: 1,
		OutputOwners: OutputOwners{
			Locktime:  1,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.ShortEmpty,
			},
		},
	}
	err := out.Verify()
	if err != nil {
		t.Fatal(err)
	}
}

func TestOutputVerifyNil(t *testing.T) {
	out := (*TransferOutput)(nil)
	err := out.Verify()
	if err == nil {
		t.Fatalf("Should have errored with a nil output")
	}
}

func TestOutputVerifyNoValue(t *testing.T) {
	out := TransferOutput{
		Amt: 0,
		OutputOwners: OutputOwners{
			Locktime:  1,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.ShortEmpty,
			},
		},
	}
	err := out.Verify()
	if err == nil {
		t.Fatalf("Should have errored with a no value output")
	}
}

func TestOutputVerifyUnspendable(t *testing.T) {
	out := TransferOutput{
		Amt: 1,
		OutputOwners: OutputOwners{
			Locktime:  1,
			Threshold: 2,
			Addrs: []ids.ShortID{
				ids.ShortEmpty,
			},
		},
	}
	err := out.Verify()
	if err == nil {
		t.Fatalf("Should have errored with an unspendable output")
	}
}

func TestOutputVerifyUnoptimized(t *testing.T) {
	out := TransferOutput{
		Amt: 1,
		OutputOwners: OutputOwners{
			Locktime:  1,
			Threshold: 0,
			Addrs: []ids.ShortID{
				ids.ShortEmpty,
			},
		},
	}
	err := out.Verify()
	if err == nil {
		t.Fatalf("Should have errored with an unoptimized output")
	}
}

func TestOutputVerifyUnsorted(t *testing.T) {
	out := TransferOutput{
		Amt: 1,
		OutputOwners: OutputOwners{
			Locktime:  1,
			Threshold: 1,
			Addrs: []ids.ShortID{
				{1},
				{0},
			},
		},
	}
	err := out.Verify()
	if err == nil {
		t.Fatalf("Should have errored with an unsorted output")
	}
}

func TestOutputVerifyDuplicated(t *testing.T) {
	out := TransferOutput{
		Amt: 1,
		OutputOwners: OutputOwners{
			Locktime:  1,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.ShortEmpty,
				ids.ShortEmpty,
			},
		},
	}
	err := out.Verify()
	if err == nil {
		t.Fatalf("Should have errored with a duplicated output")
	}
}

func TestOutputSerialize(t *testing.T) {
	c := linearcodec.NewDefault()
	m := codec.NewDefaultManager()
	if err := m.RegisterCodec(0, c); err != nil {
		t.Fatal(err)
	}

	expected := []byte{
		// Codec version
		0x00, 0x00,
		// amount:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x30, 0x39,
		// locktime:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xd4, 0x31,
		// threshold:
		0x00, 0x00, 0x00, 0x01,
		// number of addresses:
		0x00, 0x00, 0x00, 0x02,
		// addrs[0]:
		0x51, 0x02, 0x5c, 0x61, 0xfb, 0xcf, 0xc0, 0x78,
		0xf6, 0x93, 0x34, 0xf8, 0x34, 0xbe, 0x6d, 0xd2,
		0x6d, 0x55, 0xa9, 0x55,
		// addrs[1]:
		0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
		0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
		0x43, 0xab, 0x08, 0x59,
	}
	out := TransferOutput{
		Amt: 12345,
		OutputOwners: OutputOwners{
			Locktime:  54321,
			Threshold: 1,
			Addrs: []ids.ShortID{
				{
					0x51, 0x02, 0x5c, 0x61, 0xfb, 0xcf, 0xc0, 0x78,
					0xf6, 0x93, 0x34, 0xf8, 0x34, 0xbe, 0x6d, 0xd2,
					0x6d, 0x55, 0xa9, 0x55,
				},
				{
					0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
					0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
					0x43, 0xab, 0x08, 0x59,
				},
			},
		},
	}
	err := out.Verify()
	if err != nil {
		t.Fatal(err)
	}

	result, err := m.Marshal(0, &out)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(expected, result) {
		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
	}
}

func TestOutputAddresses(t *testing.T) {
	out := TransferOutput{
		Amt: 12345,
		OutputOwners: OutputOwners{
			Locktime:  54321,
			Threshold: 1,
			Addrs: []ids.ShortID{
				{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
					0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13,
				},
				{
					0x14, 0x15, 0x16, 0x17,
					0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
				},
			},
		},
	}
	err := out.Verify()
	if err != nil {
		t.Fatal(err)
	}

	addrs := out.Addresses()
	if len(addrs) != 2 {
		t.Fatalf("Wrong number of addresses")
	}

	if addr := addrs[0]; !bytes.Equal(addr, out.Addrs[0].Bytes()) {
		t.Fatalf("Wrong address returned")
	}
	if addr := addrs[1]; !bytes.Equal(addr, out.Addrs[1].Bytes()) {
		t.Fatalf("Wrong address returned")
	}
}

func TestTransferOutputState(t *testing.T) {
	intf := interface{}(&TransferOutput{})
	if _, ok := intf.(verify.State); !ok {
		t.Fatalf("should be marked as state")
	}
}
