// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txtest provides test helpers for using [tx.Tx].
package txtest

import (
	"encoding/binary"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// F works like [testing.F], but allows for the usage of [tx.Tx].
//
// This type should be used over [testing.F] when it is desired to consistently
// produce a parseable transaction.
//
// [F.Addresses] and [F.AssetIDs] are propagated to the [Fuzzer] used for each
// fuzz iteration.
type F struct {
	*testing.F

	// Addresses, if non-empty, biases [Fuzzer.Address] toward this alphabet.
	Addresses []common.Address
	// AssetIDs, if non-empty, biases [Fuzzer.AssetID] toward this alphabet.
	AssetIDs []ids.ID
}

// Add works like [testing.F.Add], but expects a [tx.Tx].
func (f *F) Add(tx *tx.Tx) {
	var e encoder
	e.unsigned(tx.Unsigned)
	encodeSlice(&e, tx.Creds, (*encoder).credential)
	f.F.Add([]byte(e))
}

// Fuzz works like [testing.F.Fuzz], but provides a [tx.Tx].
func (f *F) Fuzz(test func(t *testing.T, tx *tx.Tx)) {
	f.F.Fuzz(func(t *testing.T, data []byte) {
		fuzz := Fuzzer{
			Data:      data,
			Addresses: f.Addresses,
			AssetIDs:  f.AssetIDs,
		}
		genTx := fuzz.Tx()

		// genTx isn't always ideally formatted, so we round-trip through
		// parsing before providing it to the test body.
		bytes, err := genTx.Bytes()
		if err != nil {
			t.Skipf("invalid tx: %s", err)
		}
		tx, err := tx.Parse(bytes)
		require.NoError(t, err, "Parse()")
		test(t, tx)
	})
}

// Fuzzer turns a byte stream into structured data.
//
// The byte stream in [Fuzzer.Data] is consumed left-to-right; once exhausted,
// methods return the zero value of their result type.
type Fuzzer struct {
	// Data is the byte stream backing the fuzzer.
	Data []byte
	// Addresses, if non-empty, biases [Fuzzer.Address] toward this alphabet.
	// When empty, [Fuzzer.Address] always returns a fully random address
	// consumed from [Fuzzer.Data].
	Addresses []common.Address
	// AssetIDs, if non-empty, biases [Fuzzer.AssetID] toward this alphabet.
	// When empty, [Fuzzer.AssetID] always returns a fully random ID consumed
	// from [Fuzzer.Data].
	AssetIDs []ids.ID
}

// Bytes returns a slice of n random bytes.
//
// Once the fuzzer is exhausted, the returned bytes are zeroes.
func (f *Fuzzer) Bytes(n int) []byte {
	out := make([]byte, n)
	copy(out, f.Data)
	if n >= len(f.Data) {
		f.Data = nil
	} else {
		f.Data = f.Data[n:]
	}
	return out
}

// Bool returns a random bool.
//
// Once the fuzzer is exhausted, false is returned.
func (f *Fuzzer) Bool() bool { return f.Bytes(1)[0]&1 != 0 }

// Uint32 returns a random uint32.
//
// Once the fuzzer is exhausted, 0 is returned.
func (f *Fuzzer) Uint32() uint32 { return binary.BigEndian.Uint32(f.Bytes(4)) }

// Uint64 returns a random uint64.
//
// Once the fuzzer is exhausted, 0 is returned.
func (f *Fuzzer) Uint64() uint64 { return binary.BigEndian.Uint64(f.Bytes(8)) }

// ID returns a random [ids.ID].
//
// Once the fuzzer is exhausted, [ids.Empty] is returned.
func (f *Fuzzer) ID() ids.ID { return ids.ID(f.Bytes(ids.IDLen)) }

// ShortID returns a random [ids.ShortID].
//
// Once the fuzzer is exhausted, [ids.ShortEmpty] is returned.
func (f *Fuzzer) ShortID() ids.ShortID { return ids.ShortID(f.Bytes(ids.ShortIDLen)) }

// Signature returns a random 65-byte array.
//
// Once the fuzzer is exhausted, the zero value is returned.
func (f *Fuzzer) Signature() [65]byte { return [65]byte(f.Bytes(65)) }

// Intn returns a value in [0, x).
//
// Once the fuzzer is exhausted, 0 is returned.
func (f *Fuzzer) Intn(x int) int {
	if x < 1 {
		return 0
	}
	return int(f.Uint64() % uint64(x))
}

// Element returns a random Element in s. It panics if s is empty.
//
// Once the fuzzer is exhausted, the first entry in s is returned.
func Element[T any](f *Fuzzer, s []T) T {
	return s[f.Intn(len(s))]
}

// SliceOf generates a random slice of generated entries. The length is random,
// but is typically small.
//
// Once the fuzzer is exhausted, an empty slice is returned.
func SliceOf[T any](f *Fuzzer, gen func(*Fuzzer) T) []T {
	var out []T
	// Bool defaults to false once the generation has been exausted, so this
	// will eventually terminate.
	for f.Bool() {
		out = append(out, gen(f))
	}
	return out
}

// Address returns a random [common.Address]. When [Fuzzer.Addresses] is
// non-empty, the result is biased toward that alphabet to encourage
// repeated-address code paths.
//
// Once the fuzzer is exhausted, the zero address is returned.
func (f *Fuzzer) Address() common.Address {
	if !f.Bool() || len(f.Addresses) == 0 {
		return common.Address(f.Bytes(common.AddressLength))
	}
	return Element(f, f.Addresses)
}

// AssetID returns a random [ids.ID]. When [Fuzzer.AssetIDs] is non-empty,
// the result is biased toward that alphabet to encourage repeated-asset code
// paths.
//
// Once the fuzzer is exhausted, [ids.Empty] is returned.
func (f *Fuzzer) AssetID() ids.ID {
	if !f.Bool() || len(f.AssetIDs) == 0 {
		return f.ID()
	}
	return Element(f, f.AssetIDs)
}

// TransferableInput returns a random [*avax.TransferableInput].
//
// Once the fuzzer is exhausted, a non-nil pointer to the zero value with no
// signature indices is returned.
func (f *Fuzzer) TransferableInput() *avax.TransferableInput {
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID:        f.ID(),
			OutputIndex: f.Uint32(),
		},
		Asset: avax.Asset{ID: f.AssetID()},
		In: &secp256k1fx.TransferInput{
			Amt: f.Uint64(),
			Input: secp256k1fx.Input{
				SigIndices: SliceOf(f, (*Fuzzer).Uint32),
			},
		},
	}
}

// TransferableOutput returns a random [*avax.TransferableOutput].
//
// Once the fuzzer is exhausted, a non-nil pointer to the zero value with no
// owner addresses is returned.
func (f *Fuzzer) TransferableOutput() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: f.AssetID()},
		Out: &secp256k1fx.TransferOutput{
			Amt: f.Uint64(),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  f.Uint64(),
				Threshold: f.Uint32(),
				Addrs:     SliceOf(f, (*Fuzzer).ShortID),
			},
		},
	}
}

// Input returns a random [tx.Input].
//
// Once the fuzzer is exhausted, the zero value is returned.
func (f *Fuzzer) Input() tx.Input {
	return tx.Input{
		Address: f.Address(),
		Amount:  f.Uint64(),
		AssetID: f.AssetID(),
		Nonce:   f.Uint64(),
	}
}

// Output returns a random [tx.Output].
//
// Once the fuzzer is exhausted, the zero value is returned.
func (f *Fuzzer) Output() tx.Output {
	return tx.Output{
		Address: f.Address(),
		Amount:  f.Uint64(),
		AssetID: f.AssetID(),
	}
}

// ImportTx returns a random [*tx.Import].
//
// Once the fuzzer is exhausted, a non-nil pointer to the zero value with no
// imported inputs or outputs is returned.
func (f *Fuzzer) ImportTx() *tx.Import {
	return &tx.Import{
		NetworkID:      f.Uint32(),
		BlockchainID:   f.ID(),
		SourceChain:    f.ID(),
		ImportedInputs: SliceOf(f, (*Fuzzer).TransferableInput),
		Outs:           SliceOf(f, (*Fuzzer).Output),
	}
}

// ExportTx returns a random [*tx.Export].
//
// Once the fuzzer is exhausted, a non-nil pointer to the zero value with no
// inputs or exported outputs is returned.
func (f *Fuzzer) ExportTx() *tx.Export {
	return &tx.Export{
		NetworkID:        f.Uint32(),
		BlockchainID:     f.ID(),
		DestinationChain: f.ID(),
		Ins:              SliceOf(f, (*Fuzzer).Input),
		ExportedOutputs:  SliceOf(f, (*Fuzzer).TransferableOutput),
	}
}

// Unsigned returns a random [tx.Unsigned] — either a [*tx.Import] or a
// [*tx.Export].
//
// Once the fuzzer is exhausted, an empty [*tx.Export] is returned.
func (f *Fuzzer) Unsigned() tx.Unsigned {
	if f.Bool() {
		return f.ImportTx()
	} else {
		return f.ExportTx()
	}
}

// Credential returns a random [tx.Credential].
//
// Once the fuzzer is exhausted, a [*secp256k1fx.Credential] with no
// signatures is returned.
func (f *Fuzzer) Credential() tx.Credential {
	return &secp256k1fx.Credential{
		Sigs: SliceOf(f, (*Fuzzer).Signature),
	}
}

// Tx returns a random [*tx.Tx].
//
// Once the fuzzer is exhausted, a non-nil pointer to a tx wrapping an empty
// [*tx.Export] with no credentials is returned.
func (f *Fuzzer) Tx() *tx.Tx {
	return &tx.Tx{
		Unsigned: f.Unsigned(),
		Creds:    SliceOf(f, (*Fuzzer).Credential),
	}
}

// encoder writes a byte stream that [Fuzzer] will decode into the same
// structure.
type encoder []byte

func (e *encoder) bytes(b []byte)        { *e = append(*e, b...) }
func (e *encoder) uint32(v uint32)       { *e = binary.BigEndian.AppendUint32(*e, v) }
func (e *encoder) uint64(v uint64)       { *e = binary.BigEndian.AppendUint64(*e, v) }
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

// address always picks the raw-bytes branch in [Fuzzer.Address] so the encoded
// value is independent of the alphabet.
func (e *encoder) address(v common.Address) {
	e.bool(false)
	e.bytes(v[:])
}

// assetID always picks the raw-bytes branch in [Fuzzer.AssetID] so the encoded
// value is independent of the alphabet.
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
	e.uint32(in.UTXOID.OutputIndex)
	e.assetID(in.Asset.ID)
	ti := in.In.(*secp256k1fx.TransferInput)
	e.uint64(ti.Amt)
	encodeSlice(e, ti.Input.SigIndices, (*encoder).uint32)
}

func (e *encoder) transferableOutput(out *avax.TransferableOutput) {
	e.assetID(out.Asset.ID)
	to := out.Out.(*secp256k1fx.TransferOutput)
	e.uint64(to.Amt)
	e.uint64(to.OutputOwners.Locktime)
	e.uint32(to.OutputOwners.Threshold)
	encodeSlice(e, to.OutputOwners.Addrs, (*encoder).shortID)
}

func (e *encoder) input(i tx.Input) {
	e.address(i.Address)
	e.uint64(i.Amount)
	e.assetID(i.AssetID)
	e.uint64(i.Nonce)
}

func (e *encoder) output(o tx.Output) {
	e.address(o.Address)
	e.uint64(o.Amount)
	e.assetID(o.AssetID)
}

func (e *encoder) importTx(t *tx.Import) {
	e.uint32(t.NetworkID)
	e.id(t.BlockchainID)
	e.id(t.SourceChain)
	encodeSlice(e, t.ImportedInputs, (*encoder).transferableInput)
	encodeSlice(e, t.Outs, (*encoder).output)
}

func (e *encoder) exportTx(t *tx.Export) {
	e.uint32(t.NetworkID)
	e.id(t.BlockchainID)
	e.id(t.DestinationChain)
	encodeSlice(e, t.Ins, (*encoder).input)
	encodeSlice(e, t.ExportedOutputs, (*encoder).transferableOutput)
}

func (e *encoder) unsigned(u tx.Unsigned) {
	switch u := u.(type) {
	case *tx.Import:
		e.bool(true)
		e.importTx(u)
	case *tx.Export:
		e.bool(false)
		e.exportTx(u)
	}
}

func (e *encoder) credential(c tx.Credential) {
	encodeSlice(e, c.Self().Sigs, (*encoder).signature)
}

func (e *encoder) tx(tx *tx.Tx) {
	e.unsigned(tx.Unsigned)
	encodeSlice(e, tx.Creds, (*encoder).credential)
}
