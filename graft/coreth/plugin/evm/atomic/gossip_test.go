// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/stretchr/testify/require"
)

func TestGossipAtomicTxMarshaller(t *testing.T) {
	require := require.New(t)

	want := &Tx{
		UnsignedAtomicTx: &UnsignedImportTx{},
		Creds:            []verify.Verifiable{},
	}
	marshaller := TxMarshaller{}

	key0, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	require.NoError(want.Sign(Codec, [][]*secp256k1.PrivateKey{{key0}}))

	bytes, err := marshaller.MarshalGossip(want)
	require.NoError(err)

	got, err := marshaller.UnmarshalGossip(bytes)
	require.NoError(err)
	require.Equal(want.GossipID(), got.GossipID())
}
