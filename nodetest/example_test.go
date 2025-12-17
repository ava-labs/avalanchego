// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"testing"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/example/xsvm"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/tx"

	xsvmgenesis "github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	"time"
)

func TestExample(t *testing.T) {
	sk, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	genesis := xsvmgenesis.Genesis{
		Timestamp: 0,
		Allocations: []xsvmgenesis.Allocation{
			{
				Address: sk.Address(),
				Balance: 1_000_000_000,
			},
		},
	}

	genesisBytes, err := tx.Codec.Marshal(tx.CodecVersion, &genesis)
	require.NoError(t, err)

	subnet := NewSubnet[*xsvm.VM](
		t,
		5,
		&xsvm.Factory{},
		[]*secp256k1.PrivateKey{sk},
		genesisBytes,
	)

	// You interact with the VM api directly!
	blkID, err := subnet.Validators[0].VM.LastAccepted(t.Context())
	require.NoError(t, err)

	genesisBlk, err := subnet.Validators[0].VM.GetBlock(t.Context(), blkID)
	require.NoError(t, err)
	require.Equal(t, uint64(0), genesisBlk.Height())

	time.Sleep(time.Hour)
	t.Fail()
	//
	// exportTx := &tx.Export{
	// 	ChainID:     subnet.ChainID,
	// 	Nonce:       0,
	// 	MaxFee:      1_000,
	// 	PeerChainID: ids.GenerateTestID(),
	// 	IsReturn:    false,
	// 	Amount:      123,
	// 	To:          ids.GenerateTestShortID(),
	// }
	//
	// signedTx, err := tx.Sign(exportTx, sk)
	// require.NoError(t, err)
	//
	// peerBlk := &block.Stateless{
	// 	ParentID:  genesisBlk.ID(),
	// 	Timestamp: genesisBlk.Timestamp().Unix() + 1,
	// 	Height:    genesisBlk.Height() + 1,
	// 	Txs:       []*tx.Tx{signedTx},
	// }
	//
	// blkBytes, err := tx.Codec.Marshal(tx.CodecVersion, peerBlk)
	// require.NoError(t, err)
	//
	// Accept(t, chainID, Block{Bytes: blkBytes, Height: peerBlk.Height}, node)
	//
	// require.Eventually(
	// 	t,
	// 	func() bool {
	// 		got, err := vm.LastAccepted(t.Context())
	// 		require.NoError(t, err)
	//
	// 		want, err := peerBlk.ID()
	// 		require.NoError(t, err)
	//
	// 		return bytes.Equal(got[:], want[:])
	// 	},
	// 	10*time.Second,
	// 	time.Second,
	// )
}
