// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestNewImportTx(t *testing.T) {
	env := newEnvironment(false /*=postBanff*/, false /*=postCortina*/)
	defer func() {
		require.NoError(t, shutdownEnvironment(env))
	}()

	type test struct {
		description   string
		sourceChainID ids.ID
		sharedMemory  atomic.SharedMemory
		sourceKeys    []*secp256k1.PrivateKey
		timestamp     time.Time
		expectedErr   error
	}

	factory := secp256k1.Factory{}
	sourceKey, err := factory.NewPrivateKey()
	require.NoError(t, err)

	cnt := new(byte)

	// Returns a shared memory where GetDatabase returns a database
	// where [recipientKey] has a balance of [amt]
	fundedSharedMemory := func(peerChain ids.ID, assets map[ids.ID]uint64) atomic.SharedMemory {
		*cnt++
		m := atomic.NewMemory(prefixdb.New([]byte{*cnt}, env.baseDB))

		sm := m.NewSharedMemory(env.ctx.ChainID)
		peerSharedMemory := m.NewSharedMemory(peerChain)

		for assetID, amt := range assets {
			// #nosec G404
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        ids.GenerateTestID(),
					OutputIndex: rand.Uint32(),
				},
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amt,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Addrs:     []ids.ShortID{sourceKey.PublicKey().Address()},
						Threshold: 1,
					},
				},
			}
			utxoBytes, err := txs.Codec.Marshal(txs.Version, utxo)
			require.NoError(t, err)

			inputID := utxo.InputID()
			require.NoError(t, peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
				env.ctx.ChainID: {
					PutRequests: []*atomic.Element{
						{
							Key:   inputID[:],
							Value: utxoBytes,
							Traits: [][]byte{
								sourceKey.PublicKey().Address().Bytes(),
							},
						},
					},
				},
			}))
		}

		return sm
	}

	customAssetID := ids.GenerateTestID()

	tests := []test{
		{
			description:   "can't pay fee",
			sourceChainID: env.ctx.XChainID,
			sharedMemory: fundedSharedMemory(
				env.ctx.XChainID,
				map[ids.ID]uint64{
					env.ctx.AVAXAssetID: env.config.TxFee - 1,
				},
			),
			sourceKeys:  []*secp256k1.PrivateKey{sourceKey},
			expectedErr: utxo.ErrInsufficientFunds,
		},
		{
			description:   "can barely pay fee",
			sourceChainID: env.ctx.XChainID,
			sharedMemory: fundedSharedMemory(
				env.ctx.XChainID,
				map[ids.ID]uint64{
					env.ctx.AVAXAssetID: env.config.TxFee,
				},
			),
			sourceKeys:  []*secp256k1.PrivateKey{sourceKey},
			expectedErr: nil,
		},
		{
			description:   "attempting to import from C-chain",
			sourceChainID: cChainID,
			sharedMemory: fundedSharedMemory(
				cChainID,
				map[ids.ID]uint64{
					env.ctx.AVAXAssetID: env.config.TxFee,
				},
			),
			sourceKeys:  []*secp256k1.PrivateKey{sourceKey},
			timestamp:   env.config.ApricotPhase5Time,
			expectedErr: nil,
		},
		{
			description:   "attempting to import non-avax from X-chain",
			sourceChainID: env.ctx.XChainID,
			sharedMemory: fundedSharedMemory(
				env.ctx.XChainID,
				map[ids.ID]uint64{
					env.ctx.AVAXAssetID: env.config.TxFee,
					customAssetID:       1,
				},
			),
			sourceKeys:  []*secp256k1.PrivateKey{sourceKey},
			timestamp:   env.config.BanffTime,
			expectedErr: nil,
		},
	}

	to := ids.GenerateTestShortID()
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			require := require.New(t)

			env.msm.SharedMemory = tt.sharedMemory
			tx, err := env.txBuilder.NewImportTx(
				tt.sourceChainID,
				to,
				tt.sourceKeys,
				ids.ShortEmpty,
			)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.NoError(err)

			unsignedTx := tx.Unsigned.(*txs.ImportTx)
			require.NotEmpty(unsignedTx.ImportedInputs)
			numInputs := len(unsignedTx.Ins) + len(unsignedTx.ImportedInputs)
			require.Equal(len(tx.Creds), numInputs, "should have the same number of credentials as inputs")

			totalIn := uint64(0)
			for _, in := range unsignedTx.Ins {
				totalIn += in.Input().Amount()
			}
			for _, in := range unsignedTx.ImportedInputs {
				totalIn += in.Input().Amount()
			}
			totalOut := uint64(0)
			for _, out := range unsignedTx.Outs {
				totalOut += out.Out.Amount()
			}

			require.Equal(env.config.TxFee, totalIn-totalOut)

			fakedState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			fakedState.SetTimestamp(tt.timestamp)

			fakedParent := ids.GenerateTestID()
			env.SetState(fakedParent, fakedState)

			verifier := MempoolTxVerifier{
				Backend:       &env.backend,
				ParentID:      fakedParent,
				StateVersions: env,
				Tx:            tx,
			}
			require.NoError(tx.Unsigned.Visit(&verifier))
		})
	}
}
