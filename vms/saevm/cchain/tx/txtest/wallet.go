// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txtest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Signature can be used within a [secp256k1fx.Credential] to authorize a
// transaction.
type Signature = [secp256k1.SignatureLen]byte

// NewKey returns a freshly-generated [secp256k1.PrivateKey].
func NewKey(tb testing.TB) *secp256k1.PrivateKey {
	tb.Helper()

	sk, err := secp256k1.NewPrivateKey()
	require.NoError(tb, err, "secp256k1.NewPrivateKey()")
	return sk
}

// Sign signs u with s and returns the signature.
func Sign(tb testing.TB, u tx.Unsigned, s keychain.Signer) Signature {
	tb.Helper()

	b, err := tx.UnsignedBytes(u)
	require.NoErrorf(tb, err, "tx.UnsignedBytes(%T)", u)
	sig, err := s.Sign(b)
	require.NoErrorf(tb, err, "%T.Sign(%T)", s, u)
	require.Lenf(tb, sig, len(Signature{}), "len(%T.Sign(%T))", s, u)
	return Signature(sig)
}

// MarshalUTXO returns the canonical binary format of utxo.
func MarshalUTXO(tb testing.TB, utxo *avax.UTXO) []byte {
	tb.Helper()

	b, err := tx.MarshalUTXO(utxo)
	require.NoError(tb, err, "tx.MarshalUTXO()")
	return b
}

// ParseUTXO deserializes an [avax.UTXO] from its canonical binary format.
func ParseUTXO(tb testing.TB, b []byte) *avax.UTXO {
	tb.Helper()

	utxo, err := tx.ParseUTXO(b)
	require.NoError(tb, err, "tx.ParseUTXO()")
	return utxo
}

// NewTransferOutput returns a single-owner [secp256k1fx.TransferOutput] with
// threshold 1 paying amt to addr.
func NewTransferOutput(amt uint64, addr ids.ShortID) *secp256k1fx.TransferOutput {
	return &secp256k1fx.TransferOutput{
		Amt: amt,
		OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
	}
}

// NewUTXO returns a new [avax.UTXO] containing a single-owner output of amt of
// assetID held by addr.
func NewUTXO(amt uint64, assetID ids.ID, addr ids.ShortID) *avax.UTXO {
	return &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
		Asset:  avax.Asset{ID: assetID},
		Out:    NewTransferOutput(amt, addr),
	}
}

// ExportedUTXOs returns the UTXOs produced by e when wrapped in a [tx.Tx]
// with the given txID.
func ExportedUTXOs(txID ids.ID, e *tx.Export) []*avax.UTXO {
	utxos := make([]*avax.UTXO, len(e.ExportedOutputs))
	for i, out := range e.ExportedOutputs {
		utxos[i] = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i), //#nosec G115 -- Won't overflow
			},
			Asset: out.Asset,
			Out:   out.Out,
		}
	}
	return utxos
}
