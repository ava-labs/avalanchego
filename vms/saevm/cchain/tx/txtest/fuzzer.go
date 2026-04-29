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
type F struct {
	*testing.F

	// Addresses, if non-empty, biases addresses toward this alphabet.
	Addresses []common.Address
	// AssetIDs, if non-empty, biases assetIDs toward this alphabet.
	AssetIDs []ids.ID
}

// Add works like [testing.F.Add], but expects a [tx.Tx].
func (f *F) Add(tx *tx.Tx) {
	var e encoder
	e.unsigned(tx.Unsigned)
	sliceTo(&e, tx.Creds, (*encoder).credential)
	f.F.Add([]byte(e))
}

// Fuzz works like [testing.F.Fuzz], but provides a [tx.Tx].
func (f *F) Fuzz(test func(t *testing.T, tx *tx.Tx)) {
	f.F.Fuzz(func(t *testing.T, data []byte) {
		fuzz := decoder{
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

// decoder turns a byte stream into structured data.
//
// The byte stream is consumed as structured data is produced; once exhausted,
// methods return the zero value of their result type.
type decoder struct {
	// Data is the byte stream backing the decoder.
	Data []byte
	// Addresses, if non-empty, biases [decoder.Address] toward this alphabet.
	// When empty, [decoder.Address] always returns a fully random address
	// consumed from data.
	Addresses []common.Address
	// AssetIDs, if non-empty, biases [decoder.AssetID] toward this alphabet.
	// When empty, [decoder.AssetID] always returns a fully random ID consumed
	// from data.
	AssetIDs []ids.ID
}

// Bytes returns a slice of n random bytes.
//
// Once the fuzzer is exhausted, the returned bytes are zeroes.
func (d *decoder) Bytes(n int) []byte {
	out := make([]byte, n)
	copy(out, d.Data)
	if n >= len(d.Data) {
		d.Data = nil
	} else {
		d.Data = d.Data[n:]
	}
	return out
}

// Bool returns a random bool.
//
// Once the fuzzer is exhausted, false is returned.
func (d *decoder) Bool() bool { return d.Bytes(1)[0]&1 != 0 }

// Uint32 returns a random uint32.
//
// Once the fuzzer is exhausted, 0 is returned.
func (d *decoder) Uint32() uint32 { return binary.BigEndian.Uint32(d.Bytes(4)) }

// Uint64 returns a random uint64.
//
// Once the fuzzer is exhausted, 0 is returned.
func (d *decoder) Uint64() uint64 { return binary.BigEndian.Uint64(d.Bytes(8)) }

// ID returns a random [ids.ID].
//
// Once the fuzzer is exhausted, [ids.Empty] is returned.
func (d *decoder) ID() ids.ID { return ids.ID(d.Bytes(ids.IDLen)) }

// ShortID returns a random [ids.ShortID].
//
// Once the fuzzer is exhausted, [ids.ShortEmpty] is returned.
func (d *decoder) ShortID() ids.ShortID { return ids.ShortID(d.Bytes(ids.ShortIDLen)) }

// Signature returns a random 65-byte array.
//
// Once the fuzzer is exhausted, the zero value is returned.
func (d *decoder) Signature() [65]byte { return [65]byte(d.Bytes(65)) }

// Intn returns a value in [0, x).
//
// Once the fuzzer is exhausted, 0 is returned.
func (d *decoder) Intn(x int) int {
	if x < 1 {
		return 0
	}
	return int(d.Uint64() % uint64(x))
}

// element returns a random element in s. It panics if s is empty.
//
// Once the fuzzer is exhausted, the first entry in s is returned.
func element[T any](f *decoder, s []T) T {
	return s[f.Intn(len(s))]
}

// sliceOf generates a random slice of generated entries. The length is random,
// but is typically small.
//
// Once the fuzzer is exhausted, an empty slice is returned.
func sliceOf[T any](f *decoder, gen func(*decoder) T) []T {
	var out []T
	// Bool defaults to false once the generation has been exausted, so this
	// will eventually terminate.
	for f.Bool() {
		out = append(out, gen(f))
	}
	return out
}

// Address returns a random [common.Address]. When [fuzzer.Addresses] is
// non-empty, the result is biased toward that alphabet to encourage
// repeated-address code paths.
//
// Once the fuzzer is exhausted, the zero address is returned.
func (d *decoder) Address() common.Address {
	if !d.Bool() || len(d.Addresses) == 0 {
		return common.Address(d.Bytes(common.AddressLength))
	}
	return element(d, d.Addresses)
}

// AssetID returns a random [ids.ID]. When [fuzzer.AssetIDs] is non-empty,
// the result is biased toward that alphabet to encourage repeated-asset code
// paths.
//
// Once the fuzzer is exhausted, [ids.Empty] is returned.
func (d *decoder) AssetID() ids.ID {
	if !d.Bool() || len(d.AssetIDs) == 0 {
		return d.ID()
	}
	return element(d, d.AssetIDs)
}

// TransferableInput returns a random [avax.TransferableInput].
//
// Once the fuzzer is exhausted, a non-nil pointer to the zero value with no
// signature indices is returned.
func (d *decoder) TransferableInput() *avax.TransferableInput {
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID:        d.ID(),
			OutputIndex: d.Uint32(),
		},
		Asset: avax.Asset{ID: d.AssetID()},
		In: &secp256k1fx.TransferInput{
			Amt: d.Uint64(),
			Input: secp256k1fx.Input{
				SigIndices: sliceOf(d, (*decoder).Uint32),
			},
		},
	}
}

// TransferableOutput returns a random [avax.TransferableOutput].
//
// Once the fuzzer is exhausted, a non-nil pointer to the zero value with no
// owner addresses is returned.
func (d *decoder) TransferableOutput() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: d.AssetID()},
		Out: &secp256k1fx.TransferOutput{
			Amt: d.Uint64(),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  d.Uint64(),
				Threshold: d.Uint32(),
				Addrs:     sliceOf(d, (*decoder).ShortID),
			},
		},
	}
}

// Input returns a random [tx.Input].
//
// Once the fuzzer is exhausted, the zero value is returned.
func (d *decoder) Input() tx.Input {
	return tx.Input{
		Address: d.Address(),
		Amount:  d.Uint64(),
		AssetID: d.AssetID(),
		Nonce:   d.Uint64(),
	}
}

// Output returns a random [tx.Output].
//
// Once the fuzzer is exhausted, the zero value is returned.
func (d *decoder) Output() tx.Output {
	return tx.Output{
		Address: d.Address(),
		Amount:  d.Uint64(),
		AssetID: d.AssetID(),
	}
}

// ImportTx returns a random [*tx.Import].
//
// Once the fuzzer is exhausted, a non-nil pointer to the zero value with no
// imported inputs or outputs is returned.
func (d *decoder) ImportTx() *tx.Import {
	return &tx.Import{
		NetworkID:      d.Uint32(),
		BlockchainID:   d.ID(),
		SourceChain:    d.ID(),
		ImportedInputs: sliceOf(d, (*decoder).TransferableInput),
		Outs:           sliceOf(d, (*decoder).Output),
	}
}

// ExportTx returns a random [*tx.Export].
//
// Once the fuzzer is exhausted, a non-nil pointer to the zero value with no
// inputs or exported outputs is returned.
func (d *decoder) ExportTx() *tx.Export {
	return &tx.Export{
		NetworkID:        d.Uint32(),
		BlockchainID:     d.ID(),
		DestinationChain: d.ID(),
		Ins:              sliceOf(d, (*decoder).Input),
		ExportedOutputs:  sliceOf(d, (*decoder).TransferableOutput),
	}
}

// Unsigned returns a random [tx.Unsigned] — either a [*tx.Import] or a
// [*tx.Export].
//
// Once the fuzzer is exhausted, an empty [*tx.Export] is returned.
func (d *decoder) Unsigned() tx.Unsigned {
	if d.Bool() {
		return d.ImportTx()
	} else {
		return d.ExportTx()
	}
}

// Credential returns a random [tx.Credential].
//
// Once the fuzzer is exhausted, a [*secp256k1fx.Credential] with no
// signatures is returned.
func (d *decoder) Credential() tx.Credential {
	return &secp256k1fx.Credential{
		Sigs: sliceOf(d, (*decoder).Signature),
	}
}

// Tx returns a random [*tx.Tx].
//
// Once the fuzzer is exhausted, a non-nil pointer to a tx wrapping an empty
// [*tx.Export] with no credentials is returned.
func (d *decoder) Tx() *tx.Tx {
	return &tx.Tx{
		Unsigned: d.Unsigned(),
		Creds:    sliceOf(d, (*decoder).Credential),
	}
}

// encoder writes a byte stream that [fuzzer] will decode into the same
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

// address always picks the raw-bytes branch in [fuzzer.Address] so the encoded
// value is independent of the alphabet.
func (e *encoder) address(v common.Address) {
	e.bool(false)
	e.bytes(v[:])
}

// assetID always picks the raw-bytes branch in [fuzzer.AssetID] so the encoded
// value is independent of the alphabet.
func (e *encoder) assetID(v ids.ID) {
	e.bool(false)
	e.id(v)
}

func sliceTo[T any](e *encoder, items []T, gen func(*encoder, T)) {
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
	sliceTo(e, ti.Input.SigIndices, (*encoder).uint32)
}

func (e *encoder) transferableOutput(out *avax.TransferableOutput) {
	e.assetID(out.Asset.ID)
	to := out.Out.(*secp256k1fx.TransferOutput)
	e.uint64(to.Amt)
	e.uint64(to.OutputOwners.Locktime)
	e.uint32(to.OutputOwners.Threshold)
	sliceTo(e, to.OutputOwners.Addrs, (*encoder).shortID)
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
	sliceTo(e, t.ImportedInputs, (*encoder).transferableInput)
	sliceTo(e, t.Outs, (*encoder).output)
}

func (e *encoder) exportTx(t *tx.Export) {
	e.uint32(t.NetworkID)
	e.id(t.BlockchainID)
	e.id(t.DestinationChain)
	sliceTo(e, t.Ins, (*encoder).input)
	sliceTo(e, t.ExportedOutputs, (*encoder).transferableOutput)
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
	sliceTo(e, c.Self().Sigs, (*encoder).signature)
}

func (e *encoder) tx(tx *tx.Tx) {
	e.unsigned(tx.Unsigned)
	sliceTo(e, tx.Creds, (*encoder).credential)
}
