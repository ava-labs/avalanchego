// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestMakeKeys(t *testing.T) {
	require := require.New(t)

	cert, err := NewTLSCert()
	require.NoError(err)

	msg := []byte(fmt.Sprintf("msg %d", time.Now().Unix()))
	msgHash := hashing.ComputeHash256(msg)

	sig, err := cert.PrivateKey.(crypto.Signer).Sign(rand.Reader, msgHash, crypto.SHA256)
	require.NoError(err)

	err = cert.Leaf.CheckSignature(cert.Leaf.SignatureAlgorithm, msg, sig)
	require.NoError(err)
}
