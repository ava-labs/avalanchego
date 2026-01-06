// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"crypto/ecdsa"
	"crypto/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"
)

// Key contains an ecdsa private key field as well as an address field
// obtained from converting the ecdsa public key.
type Key struct {
	Address    common.Address
	PrivateKey *ecdsa.PrivateKey
}

// NewKey generates a new key pair and returns a pointer to a [Key].
func NewKey(t *testing.T) *Key {
	t.Helper()
	privateKeyECDSA, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	require.NoError(t, err)
	return &Key{
		Address:    crypto.PubkeyToAddress(privateKeyECDSA.PublicKey),
		PrivateKey: privateKeyECDSA,
	}
}
