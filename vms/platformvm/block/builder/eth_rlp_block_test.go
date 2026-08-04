// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	ethtypes "github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ava-labs/libevm/common"
)

// TestBuildBlockEthRLPTx drives an eth-signed transfer through the real
// issue -> mempool -> build -> verify -> accept path.
func TestBuildBlockEthRLPTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	senderKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(senderKey.PublicKey().EthAddress())
	recipient := ids.GenerateTestShortID()

	// Fund the sender's eth address directly in state.
	fundTxID := ids.GenerateTestID()
	env.state.AddUTXO(&avax.UTXO{
		UTXOID: avax.UTXOID{TxID: fundTxID, OutputIndex: 0},
		Asset:  avax.Asset{ID: env.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 100 * units.Avax,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{sender},
			},
		},
	})
	require.NoError(env.state.Commit())

	chainID := txs.EthRLPChainID(env.ctx.NetworkID)
	ethRecipient := ethcommon.Address(recipient)
	signed := ethtypes.MustSignNewTx(
		senderKey.ToECDSA(),
		ethtypes.LatestSignerForChainID(chainID),
		&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     0,
			GasTipCap: big.NewInt(0),
			GasFeeCap: big.NewInt(1e9),
			Gas:       500_000,
			To:        &ethRecipient,
			Value:     new(big.Int).Mul(big.NewInt(3), big.NewInt(1e18)),
		},
	)
	raw, err := signed.MarshalBinary()
	require.NoError(err)

	tx, err := txs.NewSigned(&txs.EthRLPTx{RLP: raw}, txs.Codec, nil)
	require.NoError(err)

	env.ctx.Lock.Unlock()
	require.NoError(env.network.IssueTxFromRPC(tx))
	env.ctx.Lock.Lock()

	txID := tx.ID()
	_, ok := env.mempool.Get(txID)
	require.True(ok)

	blkIntf, err := env.Builder.BuildBlock(t.Context())
	require.NoError(err)
	blk := blkIntf.(*blockexecutor.Block)
	require.Len(blk.Txs(), 1)
	require.Equal(txID, blk.Txs()[0].ID())

	require.NoError(blk.Verify(t.Context()))
	require.NoError(blk.Accept(t.Context()))

	// The recipient owns a 3 AVAX UTXO and the sender's nonce advanced.
	utxoID := avax.UTXOID{TxID: txID, OutputIndex: 0}
	utxo, err := env.state.GetUTXO(utxoID.InputID())
	require.NoError(err)
	out := utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(3*units.Avax, out.Amt)
	require.Equal([]ids.ShortID{recipient}, out.Addrs)

	nonce, err := env.state.GetNextNonce(sender)
	require.NoError(err)
	require.Equal(uint64(1), nonce)
}

// A credential-padded eth tx must never reach a block. Two of them would push
// the block past the codec limit, so every proposer would fail to serialize the
// block it just packed and the chain would stop producing blocks.
func TestBuildBlockRejectsCredentialPaddedEthTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	senderKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(senderKey.PublicKey().EthAddress())
	env.state.AddUTXO(&avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
		Asset:  avax.Asset{ID: env.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 100 * units.Avax,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{sender},
			},
		},
	})
	require.NoError(env.state.Commit())

	chainID := txs.EthRLPChainID(env.ctx.NetworkID)
	to := ethcommon.Address(ids.GenerateTestShortID())
	signed := ethtypes.MustSignNewTx(
		senderKey.ToECDSA(),
		ethtypes.LatestSignerForChainID(chainID),
		&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     0,
			GasTipCap: big.NewInt(0),
			GasFeeCap: big.NewInt(1e9),
			Gas:       1_000_000,
			To:        &to,
			Value:     big.NewInt(1e18),
		},
	)
	raw, err := signed.MarshalBinary()
	require.NoError(err)

	padded := &txs.Tx{
		Unsigned: &txs.EthRLPTx{RLP: raw},
		Creds: []verify.Verifiable{
			&secp256k1fx.Credential{Sigs: make([][secp256k1.SignatureLen]byte, 3000)},
		},
	}
	require.NoError(padded.Initialize(txs.Codec))
	require.Greater(len(padded.Bytes()), 100_000)

	// Admission refuses it, so it is never gossiped and never packed. Tx
	// verification rejects it before the mempool's own guard is reached, so
	// match on the shared reason rather than one of the two errors.
	env.ctx.Lock.Unlock()
	err = env.network.IssueTxFromRPC(padded)
	env.ctx.Lock.Lock()
	require.ErrorContains(err, "must carry no credentials")

	_, ok := env.mempool.Get(padded.ID())
	require.False(ok)
	_, err = env.Builder.BuildBlock(t.Context())
	require.ErrorIs(err, ErrNoPendingBlocks)
}
