// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestInitialStateVerifyNil(t *testing.T) {
	c := codec.NewDefault()
	numFxs := 1

	is := (*InitialState)(nil)
	if err := is.Verify(c, numFxs); err == nil {
		t.Fatalf("Should have errored due to nil initial state")
	}
}

func TestInitialStateVerifyUnknownFxID(t *testing.T) {
	c := codec.NewDefault()
	numFxs := 1

	is := InitialState{
		FxID: 1,
	}
	if err := is.Verify(c, numFxs); err == nil {
		t.Fatalf("Should have errored due to unknown FxID")
	}
}

func TestInitialStateVerifyNilOutput(t *testing.T) {
	c := codec.NewDefault()
	numFxs := 1

	is := InitialState{
		FxID: 0,
		Outs: []verify.Verifiable{nil},
	}
	if err := is.Verify(c, numFxs); err == nil {
		t.Fatalf("Should have errored due to a nil output")
	}
}

func TestInitialStateVerifyInvalidOutput(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&ava.TestVerifiable{})
	numFxs := 1

	is := InitialState{
		FxID: 0,
		Outs: []verify.Verifiable{&ava.TestVerifiable{Err: errors.New("")}},
	}
	if err := is.Verify(c, numFxs); err == nil {
		t.Fatalf("Should have errored due to an invalid output")
	}
}

func TestInitialStateVerifyUnsortedOutputs(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&ava.TestTransferable{})
	numFxs := 1

	is := InitialState{
		FxID: 0,
		Outs: []verify.Verifiable{
			&ava.TestTransferable{Val: 1},
			&ava.TestTransferable{Val: 0},
		},
	}
	if err := is.Verify(c, numFxs); err == nil {
		t.Fatalf("Should have errored due to unsorted outputs")
	}

	is.Sort(c)

	if err := is.Verify(c, numFxs); err != nil {
		t.Fatal(err)
	}
}

func TestInitialStateVerifySerialization(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&secp256k1fx.TransferOutput{})

	expected := []byte{
		// fxID:
		0x00, 0x00, 0x00, 0x00,
		// num outputs:
		0x00, 0x00, 0x00, 0x01,
		// output:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x30, 0x39, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xd4, 0x31, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x02, 0x51, 0x02, 0x5c, 0x61,
		0xfb, 0xcf, 0xc0, 0x78, 0xf6, 0x93, 0x34, 0xf8,
		0x34, 0xbe, 0x6d, 0xd2, 0x6d, 0x55, 0xa9, 0x55,
		0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
		0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
		0x43, 0xab, 0x08, 0x59,
	}

	is := &InitialState{
		FxID: 0,
		Outs: []verify.Verifiable{
			&secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  54321,
					Threshold: 1,
					Addrs: []ids.ShortID{
						ids.NewShortID([20]byte{
							0x51, 0x02, 0x5c, 0x61, 0xfb, 0xcf, 0xc0, 0x78,
							0xf6, 0x93, 0x34, 0xf8, 0x34, 0xbe, 0x6d, 0xd2,
							0x6d, 0x55, 0xa9, 0x55,
						}),
						ids.NewShortID([20]byte{
							0xc3, 0x34, 0x41, 0x28, 0xe0, 0x60, 0x12, 0x8e,
							0xde, 0x35, 0x23, 0xa2, 0x4a, 0x46, 0x1c, 0x89,
							0x43, 0xab, 0x08, 0x59,
						}),
					},
				},
			},
		},
	}

	isBytes, err := c.Marshal(is)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(isBytes, expected) {
		t.Fatalf("Expected:\n%s\nResult:\n%s",
			formatting.DumpBytes{Bytes: expected},
			formatting.DumpBytes{Bytes: isBytes},
		)
	}
}
