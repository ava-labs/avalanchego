// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"encoding/binary"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// fuzzTx works like [testing.F.Fuzz], but produces a [Tx] rather than a byte
// slice for fuzzing.
//
// This function should be used over [testing.F.Fuzz] when it is desired to
// consistently produce a parseable transaction.
func fuzzTx(f *testing.F, test func(t *testing.T, tx *Tx)) {
	for _, test := range tests {
		f.Add(test.bytes)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		f := fuzzer(data)
		genTx := f.tx()

		// genTx isn't always ideally formatted, so we round-trip through
		// parsing before providing it to the test body.
		bytes, err := genTx.Bytes()
		if err != nil {
			t.Skipf("invalid tx: %s", err)
		}
		tx, err := Parse(bytes)
		require.NoError(t, err, "Parse()")
		test(t, tx)
	})
}

// fuzzer turns a byte stream into structured data.
type fuzzer []byte

// Once the fuzzer is exhausted, bytes yields zero bytes — this keeps the
// generator deterministic on short inputs without rejecting them.
func (f *fuzzer) bytes(n int) []byte {
	b := *f
	defer func() {
		*f = b
	}()

	out := make([]byte, n)
	copy(out, b)
	if n >= len(b) {
		b = nil
	} else {
		b = b[n:]
	}
	return out
}

func (f *fuzzer) bool() bool           { return f.bytes(1)[0]&1 != 0 }
func (f *fuzzer) u32() uint32          { return binary.BigEndian.Uint32(f.bytes(4)) }
func (f *fuzzer) u64() uint64          { return binary.BigEndian.Uint64(f.bytes(8)) }
func (f *fuzzer) id() ids.ID           { return ids.ID(f.bytes(ids.IDLen)) }
func (f *fuzzer) shortID() ids.ShortID { return ids.ShortID(f.bytes(ids.ShortIDLen)) }
func (f *fuzzer) signature() [65]byte  { return [65]byte(f.bytes(65)) }

// intn returns a value in [0, x).
func (f *fuzzer) intn(x int) int {
	if x < 1 {
		return 0
	}
	return int(f.u64() % uint64(x))
}

// element returns a random element in s. It panics if s is empty.
func element[T any](f *fuzzer, s []T) T {
	return s[f.intn(len(s))]
}

// sliceOf generates a random slice of randomly generate entries.
func sliceOf[T any](f *fuzzer, gen func(*fuzzer) T) []T {
	var out []T
	// bool defaults to false once the generation has been exausted, so this
	// will eventually terminate.
	for f.bool() {
		out = append(out, gen(f))
	}
	return out
}

func (f *fuzzer) address() common.Address {
	if !f.bool() {
		return common.Address(f.bytes(common.AddressLength))
	}
	return element(f, []common.Address{
		{},
		{0x01},
		{0x02},
		common.HexToAddress("0xb8b5a87d1c05676f1f966da49151fa54dbe68c33"),
		common.HexToAddress("0xeb019ccd325ad53543a7e7e3b04828bdecf3cff6"),
	})
}

func (f *fuzzer) assetID() ids.ID {
	if !f.bool() {
		return f.id()
	}
	return element(f, []ids.ID{
		{},
		{0x01},
		{0x02},
		avaxAssetID,
	})
}

func (f *fuzzer) transferableInput() *avax.TransferableInput {
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID:        f.id(),
			OutputIndex: f.u32(),
		},
		Asset: avax.Asset{ID: f.assetID()},
		In: &secp256k1fx.TransferInput{
			Amt: f.u64(),
			Input: secp256k1fx.Input{
				SigIndices: sliceOf(f, (*fuzzer).u32),
			},
		},
	}
}

func (f *fuzzer) transferableOutput() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: f.assetID()},
		Out: &secp256k1fx.TransferOutput{
			Amt: f.u64(),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  f.u64(),
				Threshold: f.u32(),
				Addrs:     sliceOf(f, (*fuzzer).shortID),
			},
		},
	}
}

func (f *fuzzer) input() Input {
	return Input{
		Address: f.address(),
		Amount:  f.u64(),
		AssetID: f.assetID(),
		Nonce:   f.u64(),
	}
}

func (f *fuzzer) output() Output {
	return Output{
		Address: f.address(),
		Amount:  f.u64(),
		AssetID: f.assetID(),
	}
}

func (f *fuzzer) importTx() *Import {
	return &Import{
		NetworkID:      f.u32(),
		BlockchainID:   f.id(),
		SourceChain:    f.id(),
		ImportedInputs: sliceOf(f, (*fuzzer).transferableInput),
		Outs:           sliceOf(f, (*fuzzer).output),
	}
}

func (f *fuzzer) exportTx() *Export {
	return &Export{
		NetworkID:        f.u32(),
		BlockchainID:     f.id(),
		DestinationChain: f.id(),
		Ins:              sliceOf(f, (*fuzzer).input),
		ExportedOutputs:  sliceOf(f, (*fuzzer).transferableOutput),
	}
}

func (f *fuzzer) unsigned() Unsigned {
	if f.bool() {
		return f.importTx()
	} else {
		return f.exportTx()
	}
}

func (f *fuzzer) credential() Credential {
	return &secp256k1fx.Credential{
		Sigs: sliceOf(f, (*fuzzer).signature),
	}
}

func (f *fuzzer) tx() *Tx {
	return &Tx{
		Unsigned: f.unsigned(),
		Creds:    sliceOf(f, (*fuzzer).credential),
	}
}
