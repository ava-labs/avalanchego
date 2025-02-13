// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	chainID = ids.GenerateTestID()
	keys    = secp256k1.TestKeys()
	assetID = ids.GenerateTestID()
)

func TestInvalidBlock(t *testing.T) {
	require := require.New(t)

	parser, err := NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	require.NoError(err)

	_, err = parser.ParseBlock(nil)
	require.ErrorIs(err, codec.ErrCantUnpackVersion)
}

func TestStandardBlocks(t *testing.T) {
	// check standard block can be built and parsed
	require := require.New(t)

	parser, err := NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	require.NoError(err)

	blkTimestamp := time.Now()
	parentID := ids.GenerateTestID()
	height := uint64(2022)
	cm := parser.Codec()
	txs, err := createTestTxs(cm)
	require.NoError(err)

	standardBlk, err := NewStandardBlock(parentID, height, blkTimestamp, txs, cm)
	require.NoError(err)

	// parse block
	parsed, err := parser.ParseBlock(standardBlk.Bytes())
	require.NoError(err)

	// compare content
	require.Equal(standardBlk.ID(), parsed.ID())
	require.Equal(standardBlk.Parent(), parsed.Parent())
	require.Equal(standardBlk.Height(), parsed.Height())
	require.Equal(standardBlk.Bytes(), parsed.Bytes())
	require.Equal(standardBlk.Timestamp(), parsed.Timestamp())

	require.IsType(&StandardBlock{}, parsed)
	parsedStandardBlk := parsed.(*StandardBlock)

	require.Equal(txs, parsedStandardBlk.Txs())
	require.Equal(parsed.Txs(), parsedStandardBlk.Txs())
}

func createTestTxs(cm codec.Manager) ([]*txs.Tx, error) {
	countTxs := 1
	testTxs := make([]*txs.Tx, 0, countTxs)
	for i := 0; i < countTxs; i++ {
		// Create the tx
		tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(12345),
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			}},
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        ids.ID{'t', 'x', 'I', 'D'},
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: assetID},
				In: &secp256k1fx.TransferInput{
					Amt: uint64(54321),
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
			Memo: []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}}}
		if err := tx.SignSECP256K1Fx(cm, [][]*secp256k1.PrivateKey{{keys[0]}}); err != nil {
			return nil, err
		}
		testTxs = append(testTxs, tx)
	}
	return testTxs, nil
}
