// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
)

// requireStripRestore checks that stripping then restoring the inner bytes of
// [b] reproduces the original bytes (and therefore the original ID) exactly, and
// that the stripped form is actually smaller.
func requireStripRestore(require *require.Assertions, b Block, innerBytes []byte) {
	full := b.Bytes()

	stripped, err := StripInnerBytes(full)
	require.NoError(err)
	require.Less(len(stripped), len(full))

	restored, err := RestoreInnerBytes(stripped, innerBytes)
	require.NoError(err)
	require.Equal(full, restored)

	parsed, err := ParseWithoutVerification(restored, false)
	require.NoError(err)
	require.Equal(b.ID(), parsed.ID())
	require.Equal(innerBytes, parsed.Block())
}

func TestStripRestoreSignedBlock(t *testing.T) {
	require := require.New(t)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	innerBytes := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	b, err := Build(
		ids.ID{1},
		time.Unix(123, 0),
		uint64(2),
		Epoch{},
		cert,
		innerBytes,
		ids.ID{4},
		key,
		false,
	)
	require.NoError(err)

	requireStripRestore(require, b, innerBytes)
}

func TestStripRestoreGraniteBlock(t *testing.T) {
	require := require.New(t)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	innerBytes := []byte{8, 7, 6, 5, 4, 3, 2, 1}
	b, err := Build(
		ids.ID{1},
		time.Unix(123, 0),
		uint64(2),
		Epoch{PChainHeight: 5, Number: 6, StartTime: 7},
		cert,
		innerBytes,
		ids.ID{4},
		key,
		false,
	)
	require.NoError(err)

	requireStripRestore(require, b, innerBytes)
}

func TestStripRestoreUnsignedBlock(t *testing.T) {
	require := require.New(t)

	innerBytes := []byte{9, 9, 9, 9}
	b, err := BuildUnsigned(ids.ID{1}, time.Unix(123, 0), uint64(2), Epoch{}, innerBytes, false)
	require.NoError(err)

	requireStripRestore(require, b, innerBytes)
}

func TestStripRestoreOption(t *testing.T) {
	require := require.New(t)

	innerBytes := []byte{2, 4, 6, 8}
	b, err := BuildOption(ids.ID{1}, innerBytes)
	require.NoError(err)

	requireStripRestore(require, b, innerBytes)
}
