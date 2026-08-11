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

// TestParentIDOffset pins the layout assumption that [ParentID] depends on:
// that every type registered with [Codec] serializes its parent ID as its first
// field, at a fixed offset. If a new block type is registered whose parent ID
// is not first, this test fails and [ParentID] must be revisited.
func TestParentIDOffset(t *testing.T) {
	var (
		parentID     = ids.GenerateTestID()
		chainID      = ids.GenerateTestID()
		timestamp    = time.Unix(123, 0)
		pChainHeight = uint64(2)
		innerBytes   = []byte{1, 2, 3, 4, 5}
		epoch        = Epoch{PChainHeight: 1, Number: 2, StartTime: 3}
	)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	tests := []struct {
		name  string
		build func() (Block, error)
	}{
		{
			name: "statelessBlock",
			build: func() (Block, error) {
				return Build(parentID, timestamp, pChainHeight, Epoch{}, cert, innerBytes, chainID, key)
			},
		},
		{
			name: "statelessGraniteBlock",
			build: func() (Block, error) {
				return Build(parentID, timestamp, pChainHeight, epoch, cert, innerBytes, chainID, key)
			},
		},
		{
			name: "statelessBlock_unsigned",
			build: func() (Block, error) {
				return BuildUnsigned(parentID, timestamp, pChainHeight, Epoch{}, innerBytes)
			},
		},
		{
			name: "statelessGraniteBlock_unsigned",
			build: func() (Block, error) {
				return BuildUnsigned(parentID, timestamp, pChainHeight, epoch, innerBytes)
			},
		},
		{
			name: "option",
			build: func() (Block, error) {
				return BuildOption(parentID, innerBytes)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			blk, err := test.build()
			require.NoError(err)

			// The cheap path must agree with a full parse.
			got, err := ParentID(blk.Bytes())
			require.NoError(err)
			require.Equal(blk.ParentID(), got)
			require.Equal(parentID, got)

			parsed, err := ParseWithoutVerification(blk.Bytes())
			require.NoError(err)
			require.Equal(parsed.ParentID(), got)
		})
	}
}

func TestParentIDErrors(t *testing.T) {
	blk, err := BuildUnsigned(ids.GenerateTestID(), time.Unix(1, 0), 1, Epoch{}, []byte{1})
	require.NoError(t, err)
	valid := blk.Bytes()

	t.Run("too_short", func(t *testing.T) {
		for _, n := range []int{0, 1, parentIDEnd - 1} {
			_, err := ParentID(valid[:n])
			require.ErrorIs(t, err, errTooShortForParentID)
		}
	})

	t.Run("wrong_codec_version", func(t *testing.T) {
		corrupt := make([]byte, len(valid))
		copy(corrupt, valid)
		corrupt[0] = 0xff
		_, err := ParentID(corrupt)
		require.Error(t, err)
	})
}
