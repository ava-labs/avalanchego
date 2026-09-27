// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/pchain"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func newVMWithWallet(t *testing.T) (tests.TestContext, *pchain.VM, wallet.Wallet, *secp256k1.PrivateKey) {
	tc := tests.NewTBTestContext(t)
	key := genesistest.DefaultFundedKeys[0]
	v := pchain.NewVM(t, []*secp256k1.PrivateKey{key})
	return tc, v, newWallet(tc, v, key), key
}

func newWallet(tc tests.TestContext, v *pchain.VM, key *secp256k1.PrivateKey) wallet.Wallet {
	wallet, err := primary.MakePWallet(
		tc.DefaultContext(),
		v.Node.URI,
		secp256k1fx.NewKeychain(key),
		primary.WalletConfig{},
	)
	require.NoError(tc, err)
	return wallet
}

// balanceOf reads the key's spendable AVAX from the node's UTXOs through a
// fresh wallet, the same path a client uses.
func balanceOf(tc tests.TestContext, v *pchain.VM, key *secp256k1.PrivateKey) uint64 {
	wallet := newWallet(tc, v, key)
	balances, err := wallet.Builder().GetBalance()
	require.NoError(tc, err)
	return balances[wallet.Builder().Context().AVAXAssetID]
}

// spent returns the inputs a transaction consumed less the change it returned.
func spent(ins []*avax.TransferableInput, outs []*avax.TransferableOutput) uint64 {
	var total uint64
	for _, in := range ins {
		total += in.In.Amount()
	}
	for _, out := range outs {
		total -= out.Out.Amount()
	}
	return total
}

func TestAddValidator(t *testing.T) {
	tc, v, wallet, key := newVMWithWallet(t)
	nodes := []pchain.Node{v.Node}
	before := balanceOf(tc, v, key)

	tx := pchain.AddValidator(tc, wallet, v.Node, nodes, ids.GenerateTestNodeID(), key.Address(), genesistest.DefaultFundedKeys[1].Address())
	validatorTx := tx.Unsigned.(*platform.AddPermissionlessValidatorTx)
	require.Equal(t, before-spent(validatorTx.Ins, validatorTx.Outs), balanceOf(tc, v, key))

	v.Reopen()

	pchain.WaitForTxCommitted(tc, nodes, tx.ID())
	pchain.VerifyValidator(tc, tx, nodes)
}

func TestAddDelegator(t *testing.T) {
	tc, v, wallet, key := newVMWithWallet(t)
	nodes := []pchain.Node{v.Node}
	before := balanceOf(tc, v, key)

	tx := pchain.AddDelegator(tc, wallet, v.Node, nodes, genesistest.DefaultNodeIDs[0], time.Time{}, key.Address())
	delegatorTx := tx.Unsigned.(*platform.AddPermissionlessDelegatorTx)
	require.Equal(t, before-spent(delegatorTx.Ins, delegatorTx.Outs), balanceOf(tc, v, key))

	v.Reopen()

	pchain.WaitForTxCommitted(tc, nodes, tx.ID())
	pchain.VerifyDelegator(tc, tx, nodes)
}

func TestDelegationEndAccruesRewardToValidator(t *testing.T) {
	tc, v, wallet, key := newVMWithWallet(t)
	var (
		ctx    = tc.DefaultContext()
		client = v.Node.Client
		nodeID = genesistest.DefaultNodeIDs[0]
		// Ends well before the validator, so only the delegation is rewarded.
		end = v.ChainTime().Add(genesis.LocalParams.MinStakeDuration + time.Hour)
	)
	tx := pchain.AddDelegator(tc, wallet, v.Node, []pchain.Node{v.Node}, nodeID, end, genesistest.DefaultFundedKeys[1].Address())
	stake := tx.Unsigned.(*platform.AddPermissionlessDelegatorTx).Validator.Wght
	balanceBefore := balanceOf(tc, v, key)
	delegator, err := pchain.GetDelegator(ctx, v.Node, nodeID, tx.ID())
	require.NoError(t, err)
	require.NotNil(t, delegator.PotentialReward)
	reward := *delegator.PotentialReward
	require.Positive(t, reward)

	v.AdvanceTime(end.Add(time.Second))

	validators, err := client.GetCurrentValidators(ctx, constants.PrimaryNetworkID, []ids.NodeID{nodeID})
	require.NoError(t, err)
	require.Len(t, validators, 1)
	require.Empty(t, validators[0].Delegators)
	// Genesis validators charge a 100% delegation fee and had no earlier
	// delegators, so the whole reward accrues to the validator.
	require.NotNil(t, validators[0].AccruedDelegateeReward)
	require.Equal(t, reward, *validators[0].AccruedDelegateeReward)

	require.Equal(t, balanceBefore+stake, balanceOf(tc, v, key))
}
