// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txtest provides test helpers for using [tx.Tx].
package txtest

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/stretchr/testify/require"

	// Imported for [secp256k1fx.Credential] comment resolution.
	_ "github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Signature can be used within a [secp256k1fx.Credential] to authorize a
// transaction.
type Signature = [secp256k1.SignatureLen]byte

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
