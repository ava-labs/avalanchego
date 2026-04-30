// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txtest provides test helpers for using [tx.Tx].
package txtest

import (
	"encoding/binary"
	"testing"

	"github.com/ava-labs/libevm/common"

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
	e.tx(tx)
	f.F.Add([]byte(e))
}

// Fuzz works like [testing.F.Fuzz], but provides a [tx.Tx].
func (f *F) Fuzz(ff func(t *testing.T, tx *tx.Tx)) {
	f.F.Fuzz(func(t *testing.T, data []byte) {
		d := decoder{
			data:      data,
			addresses: f.Addresses,
			assetIDs:  f.AssetIDs,
		}
		tx := d.tx()

		// It's possible for the fuzzer to generate a tx that exceeds the codec
		// size limits.
		if _, err := tx.Bytes(); err != nil {
			t.Skipf("invalid tx: %s", err)
		}
		ff(t, tx)
	})
}

// decoder turns a byte stream into structured data.
//
// The byte stream is consumed as structured data is produced; once exhausted,
// methods return the zero value of their result type.
type decoder struct {
	// data is the byte stream backing the decoder.
	data []byte
	// addresses, if non-empty, biases [decoder.address] toward this alphabet.
	addresses []common.Address
	// assetIDs, if non-empty, biases [decoder.assetID] toward this alphabet.
	assetIDs []ids.ID
}

// bytes always returns a slice of length n, even if the decoder is exhausted.
func (d *decoder) bytes(n int) []byte {
	out := make([]byte, n)
	copy(out, d.data)
	if n >= len(d.data) {
		d.data = nil
	} else {
		d.data = d.data[n:]
	}
	return out
}

func (d *decoder) bool() bool           { return d.bytes(1)[0]&1 != 0 }
func (d *decoder) uint32() uint32       { return binary.BigEndian.Uint32(d.bytes(4)) }
func (d *decoder) uint64() uint64       { return binary.BigEndian.Uint64(d.bytes(8)) }
func (d *decoder) addr() common.Address { return common.Address(d.bytes(common.AddressLength)) }
func (d *decoder) id() ids.ID           { return ids.ID(d.bytes(ids.IDLen)) }
func (d *decoder) shortID() ids.ShortID { return ids.ShortID(d.bytes(ids.ShortIDLen)) }
func (d *decoder) signature() [65]byte  { return [65]byte(d.bytes(65)) }

// intn returns a value in [0, x).
func (d *decoder) intn(x int) int {
	if x < 1 {
		return 0
	}
	return int(d.uint64() % uint64(x)) //#nosec G115 -- Overflow is impossible.
}

// element returns a random element biased towards values in s.
func element[T any](d *decoder, s []T, gen func(*decoder) T) T {
	// [decoder.bool] must be read before checking whether or not s is empty so
	// that encoding can assume that a bool will be read.
	if !d.bool() || len(s) == 0 {
		return gen(d)
	}
	return s[d.intn(len(s))]
}

// sliceOf generates a random slice of generated entries. The length is random,
// but is typically small.
func sliceOf[T any](d *decoder, gen func(*decoder) T) []T {
	var out []T
	// [decoder.bool] returns false once the data is exhausted, so this loop
	// will eventually terminate.
	for d.bool() {
		out = append(out, gen(d))
	}
	return out
}

func (d *decoder) address() common.Address {
	return element(d, d.addresses, (*decoder).addr)
}

func (d *decoder) assetID() ids.ID {
	return element(d, d.assetIDs, (*decoder).id)
}

func (d *decoder) transferableInput() *avax.TransferableInput {
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID:        d.id(),
			OutputIndex: d.uint32(),
		},
		Asset: avax.Asset{ID: d.assetID()},
		In: &secp256k1fx.TransferInput{
			Amt: d.uint64(),
			Input: secp256k1fx.Input{
				SigIndices: sliceOf(d, (*decoder).uint32),
			},
		},
	}
}

func (d *decoder) transferableOutput() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: d.assetID()},
		Out: &secp256k1fx.TransferOutput{
			Amt: d.uint64(),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  d.uint64(),
				Threshold: d.uint32(),
				Addrs:     sliceOf(d, (*decoder).shortID),
			},
		},
	}
}

func (d *decoder) input() tx.Input {
	return tx.Input{
		Address: d.address(),
		Amount:  d.uint64(),
		AssetID: d.assetID(),
		Nonce:   d.uint64(),
	}
}

func (d *decoder) output() tx.Output {
	return tx.Output{
		Address: d.address(),
		Amount:  d.uint64(),
		AssetID: d.assetID(),
	}
}

func (d *decoder) importTx() *tx.Import {
	return &tx.Import{
		NetworkID:      d.uint32(),
		BlockchainID:   d.id(),
		SourceChain:    d.id(),
		ImportedInputs: sliceOf(d, (*decoder).transferableInput),
		Outs:           sliceOf(d, (*decoder).output),
	}
}

func (d *decoder) exportTx() *tx.Export {
	return &tx.Export{
		NetworkID:        d.uint32(),
		BlockchainID:     d.id(),
		DestinationChain: d.id(),
		Ins:              sliceOf(d, (*decoder).input),
		ExportedOutputs:  sliceOf(d, (*decoder).transferableOutput),
	}
}

func (d *decoder) unsigned() tx.Unsigned {
	if d.bool() {
		return d.importTx()
	}
	return d.exportTx()
}

func (d *decoder) credential() tx.Credential {
	return &secp256k1fx.Credential{
		Sigs: sliceOf(d, (*decoder).signature),
	}
}

func (d *decoder) tx() *tx.Tx {
	return &tx.Tx{
		Unsigned: d.unsigned(),
		Creds:    sliceOf(d, (*decoder).credential),
	}
}

// encoder writes a byte stream that [decoder] will decode into the same
// structure.
type encoder []byte

func (e *encoder) bytes(b []byte)        { *e = append(*e, b...) }
func (e *encoder) uint32(v uint32)       { *e = binary.BigEndian.AppendUint32(*e, v) }
func (e *encoder) uint64(v uint64)       { *e = binary.BigEndian.AppendUint64(*e, v) }
func (e *encoder) addr(v common.Address) { e.bytes(v[:]) }
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

// address always picks the raw-bytes branch in [element] so the encoded value
// is independent of the alphabet.
func (e *encoder) address(v common.Address) {
	e.bool(false)
	e.addr(v)
}

// assetID always picks the raw-bytes branch in [element] so the encoded value
// is independent of the alphabet.
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
