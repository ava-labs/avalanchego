// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Ensure Execute fails when there are not enough control sigs
func TestCreateChainTxInsufficientControlSigs(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ true)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	tx, err := env.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// Remove a signature
	tx.Creds[0].(*secp256k1fx.Credential).Sigs = tx.Creds[0].(*secp256k1fx.Credential).Sigs[1:]

	stateDiff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := StandardTxExecutor{
		Backend: &env.backend,
		State:   stateDiff,
		Tx:      tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.Error(err, "should have erred because a sig is missing")
}

// Ensure Execute fails when an incorrect control signature is given
func TestCreateChainTxWrongControlSig(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ true)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	tx, err := env.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// Generate new, random key to sign tx with
	factory := crypto.FactorySECP256K1R{}
	key, err := factory.NewPrivateKey()
	require.NoError(err)

	// Replace a valid signature with one from another key
	sig, err := key.SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
	require.NoError(err)
	copy(tx.Creds[0].(*secp256k1fx.Credential).Sigs[0][:], sig)

	stateDiff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := StandardTxExecutor{
		Backend: &env.backend,
		State:   stateDiff,
		Tx:      tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.Error(err, "should have failed verification because a sig is invalid")
}

// Ensure Execute fails when the Subnet the blockchain specifies as
// its validator set doesn't exist
func TestCreateChainTxNoSuchSubnet(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ true)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	tx, err := env.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	tx.Unsigned.(*txs.CreateChainTx).SubnetID = ids.GenerateTestID()

	stateDiff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := StandardTxExecutor{
		Backend: &env.backend,
		State:   stateDiff,
		Tx:      tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.Error(err, "should have failed because subnet doesn't exist")
}

// Ensure valid tx passes semanticVerify
func TestCreateChainTxValid(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ true)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	tx, err := env.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	stateDiff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := StandardTxExecutor{
		Backend: &env.backend,
		State:   stateDiff,
		Tx:      tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.NoError(err)
}

func TestCreateChainTxAP3FeeChange(t *testing.T) {
	ap3Time := defaultGenesisTime.Add(time.Hour)
	tests := []struct {
		name         string
		time         time.Time
		fee          uint64
		expectsError bool
	}{
		{
			name:         "pre-fork - correctly priced",
			time:         defaultGenesisTime,
			fee:          0,
			expectsError: false,
		},
		{
			name:         "post-fork - incorrectly priced",
			time:         ap3Time,
			fee:          100*defaultTxFee - 1*units.NanoAvax,
			expectsError: true,
		},
		{
			name:         "post-fork - correctly priced",
			time:         ap3Time,
			fee:          100 * defaultTxFee,
			expectsError: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment( /*postBanff*/ true)
			env.config.ApricotPhase3Time = ap3Time

			defer func() {
				require.NoError(shutdownEnvironment(env))
			}()
			ins, outs, _, signers, err := env.utxosHandler.Spend(preFundedKeys, 0, test.fee, ids.ShortEmpty)
			require.NoError(err)

			subnetAuth, subnetSigners, err := env.utxosHandler.Authorize(env.state, testSubnet1.ID(), preFundedKeys)
			require.NoError(err)

			signers = append(signers, subnetSigners)

			// Create the tx

			utx := &txs.CreateChainTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          ins,
					Outs:         outs,
				}},
				SubnetID:   testSubnet1.ID(),
				VMID:       constants.AVMID,
				SubnetAuth: subnetAuth,
			}
			tx := &txs.Tx{Unsigned: utx}
			err = tx.Sign(txs.Codec, signers)
			require.NoError(err)

			stateDiff, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			stateDiff.SetTimestamp(test.time)

			executor := StandardTxExecutor{
				Backend: &env.backend,
				State:   stateDiff,
				Tx:      tx,
			}
			err = tx.Unsigned.Visit(&executor)
			require.Equal(test.expectsError, err != nil)
		})
	}
}
