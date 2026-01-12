// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

var fundedSharedMemoryCalls byte

func TestNewImportTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.ApricotPhase5)

	type test struct {
		description   string
		sourceChainID ids.ID
		sharedMemory  atomic.SharedMemory
		timestamp     time.Time
		expectedErr   error
	}

	sourceKey, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	customAssetID := ids.GenerateTestID()
	// generate a constant random source generator.
	randSrc := rand.NewSource(0)
	tests := []test{
		{
			description:   "can't pay fee",
			sourceChainID: env.ctx.XChainID,
			sharedMemory: fundedSharedMemory(
				t,
				env,
				sourceKey,
				env.ctx.XChainID,
				map[ids.ID]uint64{},
				randSrc,
			),
			expectedErr: builder.ErrInsufficientFunds,
		},
		{
			description:   "can barely pay fee",
			sourceChainID: env.ctx.XChainID,
			sharedMemory: fundedSharedMemory(
				t,
				env,
				sourceKey,
				env.ctx.XChainID,
				map[ids.ID]uint64{
					env.ctx.AVAXAssetID: 1,
				},
				randSrc,
			),
			expectedErr: nil,
		},
		{
			description:   "attempting to import from C-chain",
			sourceChainID: env.ctx.CChainID,
			sharedMemory: fundedSharedMemory(
				t,
				env,
				sourceKey,
				env.ctx.CChainID,
				map[ids.ID]uint64{
					env.ctx.AVAXAssetID: 1,
				},
				randSrc,
			),
			timestamp:   env.config.UpgradeConfig.ApricotPhase5Time,
			expectedErr: nil,
		},
		{
			description:   "attempting to import non-avax from X-chain",
			sourceChainID: env.ctx.XChainID,
			sharedMemory: fundedSharedMemory(
				t,
				env,
				sourceKey,
				env.ctx.XChainID,
				map[ids.ID]uint64{
					customAssetID: 1,
				},
				randSrc,
			),
			timestamp:   env.config.UpgradeConfig.ApricotPhase5Time,
			expectedErr: nil,
		},
	}

	to := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			require := require.New(t)

			env.msm.SharedMemory = tt.sharedMemory

			wallet := newWallet(t, env, walletConfig{
				keys:     []*secp256k1.PrivateKey{sourceKey},
				chainIDs: []ids.ID{tt.sourceChainID},
			})

			tx, err := wallet.IssueImportTx(
				tt.sourceChainID,
				to,
			)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}

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

			require.Equal(totalIn, totalOut)

			stateDiff, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			stateDiff.SetTimestamp(tt.timestamp)

			feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				stateDiff,
			)
			require.NoError(err)
		})
	}
}

// Returns a shared memory where GetDatabase returns a database
// where [recipientKey] has a balance of [amt]
func fundedSharedMemory(
	t *testing.T,
	env *environment,
	sourceKey *secp256k1.PrivateKey,
	peerChain ids.ID,
	assets map[ids.ID]uint64,
	randSrc rand.Source,
) atomic.SharedMemory {
	fundedSharedMemoryCalls++
	m := atomic.NewMemory(prefixdb.New([]byte{fundedSharedMemoryCalls}, env.baseDB))

	sm := m.NewSharedMemory(env.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(peerChain)

	for assetID, amt := range assets {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: uint32(randSrc.Int63()),
			},
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amt,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{sourceKey.Address()},
					Threshold: 1,
				},
			},
		}
		utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
		require.NoError(t, err)

		inputID := utxo.InputID()
		require.NoError(t, peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
			env.ctx.ChainID: {
				PutRequests: []*atomic.Element{
					{
						Key:   inputID[:],
						Value: utxoBytes,
						Traits: [][]byte{
							sourceKey.Address().Bytes(),
						},
					},
				},
			},
		}))
	}

	return sm
}
