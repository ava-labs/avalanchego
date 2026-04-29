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
		f.Add(encodeTx(test.new))
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

// encoder writes a byte stream that [fuzzer] decodes back into the same
// structure. Used to seed the corpus with bytes that round-trip to known txs.
type encoder []byte

func (e *encoder) bytes(b []byte)        { *e = append(*e, b...) }
func (e *encoder) u32(v uint32)          { *e = binary.BigEndian.AppendUint32(*e, v) }
func (e *encoder) u64(v uint64)          { *e = binary.BigEndian.AppendUint64(*e, v) }
func (e *encoder) id(v ids.ID)           { e.bytes(v[:]) }
func (e *encoder) shortID(v ids.ShortID) { e.bytes(v[:]) }
func (e *encoder) signature(v [65]byte)  { e.bytes(v[:]) }

func (e *encoder) bool(b bool) {
	if b {
		*e = append(*e, 1)
	} else {
		*e = append(*e, 0)
	}
}

// address and assetID always pick the raw-bytes branch in [fuzzer] so the
// encoded value is independent of the alphabet ordering.
func (e *encoder) address(v common.Address) {
	e.bool(false)
	e.bytes(v[:])
}

func (e *encoder) assetID(v ids.ID) {
	e.bool(false)
	e.id(v)
}

func encodeSlice[T any](e *encoder, items []T, gen func(*encoder, T)) {
	for _, item := range items {
		e.bool(true)
		gen(e, item)
	}
	e.bool(false)
}

func (e *encoder) transferableInput(in *avax.TransferableInput) {
	e.id(in.UTXOID.TxID)
	e.u32(in.UTXOID.OutputIndex)
	e.assetID(in.Asset.ID)
	ti := in.In.(*secp256k1fx.TransferInput)
	e.u64(ti.Amt)
	encodeSlice(e, ti.Input.SigIndices, (*encoder).u32)
}

func (e *encoder) transferableOutput(out *avax.TransferableOutput) {
	e.assetID(out.Asset.ID)
	to := out.Out.(*secp256k1fx.TransferOutput)
	e.u64(to.Amt)
	e.u64(to.OutputOwners.Locktime)
	e.u32(to.OutputOwners.Threshold)
	encodeSlice(e, to.OutputOwners.Addrs, (*encoder).shortID)
}

func (e *encoder) input(i Input) {
	e.address(i.Address)
	e.u64(i.Amount)
	e.assetID(i.AssetID)
	e.u64(i.Nonce)
}

func (e *encoder) output(o Output) {
	e.address(o.Address)
	e.u64(o.Amount)
	e.assetID(o.AssetID)
}

func (e *encoder) credential(c Credential) {
	encodeSlice(e, c.Self().Sigs, (*encoder).signature)
}

func (e *encoder) importTx(t *Import) {
	e.u32(t.NetworkID)
	e.id(t.BlockchainID)
	e.id(t.SourceChain)
	encodeSlice(e, t.ImportedInputs, (*encoder).transferableInput)
	encodeSlice(e, t.Outs, (*encoder).output)
}

func (e *encoder) exportTx(t *Export) {
	e.u32(t.NetworkID)
	e.id(t.BlockchainID)
	e.id(t.DestinationChain)
	encodeSlice(e, t.Ins, (*encoder).input)
	encodeSlice(e, t.ExportedOutputs, (*encoder).transferableOutput)
}

func (e *encoder) unsigned(u Unsigned) {
	switch u := u.(type) {
	case *Import:
		e.bool(true)
		e.importTx(u)
	case *Export:
		e.bool(false)
		e.exportTx(u)
	}
}

func encodeTx(tx *Tx) []byte {
	var e encoder
	e.unsigned(tx.Unsigned)
	encodeSlice(&e, tx.Creds, (*encoder).credential)
	return e
}
