// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestMultisigAliasTxSyntacticVerify(t *testing.T) {
	ctx := defaultContext()

	memo := []byte("memo")
	bigMemo := make([]byte, 257)
	_, err := rand.Read(bigMemo)
	require.NoError(t, err)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()

	var sortedAddrs []ids.ShortID
	var unsortedAddrs []ids.ShortID
	if bytes.Compare(addr1.Bytes(), addr2.Bytes()) < 0 {
		sortedAddrs = []ids.ShortID{addr1, addr2}
		unsortedAddrs = []ids.ShortID{addr2, addr1}
	} else {
		sortedAddrs = []ids.ShortID{addr2, addr1}
		unsortedAddrs = []ids.ShortID{addr1, addr2}
	}

	baseTx := BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
	}}

	tests := map[string]struct {
		tx          *MultisigAliasTx
		expectedErr error
	}{
		"OK": {
			tx: &MultisigAliasTx{
				BaseTx: baseTx,
				MultisigAlias: multisig.Alias{
					ID:   ids.GenerateTestShortID(),
					Memo: memo,
					Owners: &secp256k1fx.OutputOwners{
						Threshold: 2,
						Addrs:     sortedAddrs,
					},
				},
				Auth: &secp256k1fx.Input{SigIndices: []uint32{0, 1}},
			},
		},
		"Unordered Owners": {
			tx: &MultisigAliasTx{
				BaseTx: baseTx,
				MultisigAlias: multisig.Alias{
					ID:   ids.GenerateTestShortID(),
					Memo: memo,
					Owners: &secp256k1fx.OutputOwners{
						Threshold: 2,
						Addrs:     unsortedAddrs,
					},
				},
				Auth: &secp256k1fx.Input{SigIndices: []uint32{0, 1}},
			},
			expectedErr: errFailedToVerifyAliasOrAuth,
		},
		"Very big memo": {
			tx: &MultisigAliasTx{
				BaseTx: baseTx,
				MultisigAlias: multisig.Alias{
					ID:   ids.GenerateTestShortID(),
					Memo: bigMemo,
					Owners: &secp256k1fx.OutputOwners{
						Threshold: 2,
						Addrs:     sortedAddrs,
					},
				},
				Auth: &secp256k1fx.Input{SigIndices: []uint32{0, 1}},
			},
			expectedErr: errFailedToVerifyAliasOrAuth,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}
