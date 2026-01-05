// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestDBAddAndSign(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	messageDB := NewDB(db)

	require.NoError(t, messageDB.Add(testUnsignedMessage))

	signature, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)

	wantSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, wantSig, signature)
}
