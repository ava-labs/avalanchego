// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/block"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/tx"

	xsvmgenesis "github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
)

func TestExample(t *testing.T) {
	sk, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	genesis := xsvmgenesis.Genesis{
		Timestamp: 0,
		Allocations: []xsvmgenesis.Allocation{
			{
				Address: sk.Address(),
				Balance: 1_000_000 * units.Avax,
			},
		},
	}

	genesisBytes, err := tx.Codec.Marshal(tx.CodecVersion, &genesis)
	require.NoError(t, err)

	subnet := NewSubnet[*xsvm.VM](
		t,
		2,
		&xsvm.Factory{},
		[]*secp256k1.PrivateKey{sk},
		genesisBytes,
	)

	// TODO racey hack - do health check for subnet bootstrap
	time.Sleep(3 * time.Second)

	// We can interact with the VM directly. This does are not 1:1 behavior with
	// because proposervm (leader election) behavior is not currently preserved.
	blkID, err := subnet.Validators[0].VM.LastAccepted(t.Context())
	require.NoError(t, err)

	t.Log(subnet.Validators[0].ID(), "has last accepted block", blkID)

	genesisBlk, err := subnet.Validators[0].VM.GetBlock(t.Context(), blkID)
	require.NoError(t, err)
	require.Equal(t, uint64(0), genesisBlk.Height())

	exportTx := &tx.Export{
		ChainID:     subnet.ChainID,
		Nonce:       0,
		MaxFee:      1_000,
		PeerChainID: ids.GenerateTestID(),
		IsReturn:    false,
		Amount:      123,
		To:          ids.GenerateTestShortID(),
	}

	signedTx, err := tx.Sign(exportTx, sk)
	require.NoError(t, err)

	peerBlk := &block.Stateless{
		ParentID:  genesisBlk.ID(),
		Timestamp: genesisBlk.Timestamp().Unix() + 1,
		Height:    genesisBlk.Height() + 1,
		Txs:       []*tx.Tx{signedTx},
	}

	blkBytes, err := tx.Codec.Marshal(tx.CodecVersion, peerBlk)
	require.NoError(t, err)

	subnet.Validators[0].Accept(
		subnet.ChainID,
		Block{Bytes: blkBytes, Height: peerBlk.Height},
	)

	// Check that each validator accepted the block
	for _, validator := range subnet.Validators {
		for {
			got, err := validator.VM.LastAccepted(t.Context())
			require.NoError(t, err)

			want, err := peerBlk.ID()
			require.NoError(t, err)

			if bytes.Equal(got[:], want[:]) {
				t.Log(validator.ID(), "accepted latest tip", got)
				break
			} else {
				t.Log(validator.ID(), "still has last accepted tip of", got)
			}

			time.Sleep(time.Second)
		}
	}
}
