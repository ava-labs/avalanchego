// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/subnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/export"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/transfer"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	pwallet "github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
)

const isolatedValidatorWeight = 100

var _ = e2e.DescribePChain("[Isolated L1]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("runs without the P-chain and later rejoins it", func() {
		var (
			env        = e2e.GetEnv(tc)
			network    = env.GetNetwork()
			keychain   = env.NewKeychain()
			pWallet    = e2e.NewWallet(tc, keychain, env.GetRandomNodeURI()).P()
			chainKey   = e2e.NewPrivateKey(tc)
			isolatedA  = tmpnet.NewEphemeralNode(tmpnet.FlagsMap{})
			isolatedB  = tmpnet.NewEphemeralNode(tmpnet.FlagsMap{})
			validators = []*tmpnet.Node{isolatedA, isolatedB}
		)

		subnetID, chainTx := createSubnetAndChain(tc, pWallet, keychain, chainKey)
		chainID := chainTx.ID()

		tc.By("starting two validators for the isolated L1", func() {
			startIsolatedValidators(tc, network, chainTx, isolatedA, isolatedB)
		})

		isolatedRecipient := e2e.NewPrivateKey(tc).Address()
		tc.By("producing blocks while isolated", func() {
			issueTransfer(tc, isolatedA, chainID, chainKey, isolatedRecipient)
			requireReceivedTransfer(tc, isolatedB, chainID, isolatedRecipient)
		})

		var exportTxID ids.ID
		tc.By("sending a warp message while isolated", func() {
			exportTx, err := export.Export(tc.DefaultContext(), &export.Config{
				URI:                isolatedA.GetAccessibleURI(),
				SourceChainID:      chainID,
				DestinationChainID: ids.GenerateTestID(),
				Amount:             units.Schmeckle,
				To:                 e2e.NewPrivateKey(tc).Address(),
				PrivateKey:         chainKey,
			})
			require.NoError(err)
			exportTxID = exportTx.TxID
		})

		tc.By("converting the subnet to an L1 with the same validators", func() {
			convertSubnetToL1(tc, pWallet, subnetID, chainID, []l1Validator{
				{node: isolatedA, weight: isolatedValidatorWeight},
				{node: isolatedB, weight: isolatedValidatorWeight},
			})
		})

		tc.By("restarting the validators without the isolated L1 flags", func() {
			restartWithoutIsolatedL1(tc, network, subnetID, validators...)
			for _, node := range validators {
				e2e.WaitForHealthy(tc, node)
			}
		})

		tc.By("producing blocks after rejoining", func() {
			recipient := e2e.NewPrivateKey(tc).Address()
			issueTransfer(tc, isolatedA, chainID, chainKey, recipient)
			requireReceivedTransfer(tc, isolatedB, chainID, recipient)
		})

		tc.By("signing warp messages after rejoining", func() {
			for _, node := range validators {
				requireSignsMessage(tc, node, chainID, exportTxID)
			}
		})

		tc.By("a new node should sync the entire L1s history after rejoining", func() {
			newNode := e2e.AddEphemeralNode(tc, network, tmpnet.NewEphemeralNode(tmpnet.FlagsMap{
				config.TrackSubnetsKey: subnetID.String(),
			}))
			e2e.WaitForHealthy(tc, newNode)

			requireReceivedTransfer(tc, newNode, chainID, isolatedRecipient)

			lastAcceptedID, _, err := api.NewClient(isolatedA.GetAccessibleURI(), chainID.String()).LastAccepted(tc.DefaultContext())
			require.NoError(err)
			tc.Eventually(func() bool {
				newNodeLastAcceptedID, _, err := api.NewClient(newNode.GetAccessibleURI(), chainID.String()).LastAccepted(tc.DefaultContext())
				return err == nil && newNodeLastAcceptedID == lastAcceptedID
			}, tests.DefaultTimeout, e2e.DefaultPollingInterval, "new node should reach the same last accepted block")
		})
	})

	ginkgo.It("rejoins the P-chain with a different validator set", func() {
		var (
			env       = e2e.GetEnv(tc)
			network   = env.GetNetwork()
			keychain  = env.NewKeychain()
			pWallet   = e2e.NewWallet(tc, keychain, env.GetRandomNodeURI()).P()
			chainKey  = e2e.NewPrivateKey(tc)
			isolatedA = tmpnet.NewEphemeralNode(tmpnet.FlagsMap{})
			isolatedB = tmpnet.NewEphemeralNode(tmpnet.FlagsMap{})
			// newValidator never runs isolated. It only becomes a validator
			// when the subnet is converted.
			newValidator = tmpnet.NewEphemeralNode(tmpnet.FlagsMap{})
		)

		subnetID, chainTx := createSubnetAndChain(tc, pWallet, keychain, chainKey)
		chainID := chainTx.ID()

		tc.By("starting two validators for the isolated L1", func() {
			startIsolatedValidators(tc, network, chainTx, isolatedA, isolatedB)
		})

		isolatedRecipient := e2e.NewPrivateKey(tc).Address()
		tc.By("producing blocks while isolated", func() {
			issueTransfer(tc, isolatedA, chainID, chainKey, isolatedRecipient)
			requireReceivedTransfer(tc, isolatedB, chainID, isolatedRecipient)
		})

		// Validator B is dropped and a new node is added. Validator A holds the chains history
		// and most of the stake, so the new validator syncs that history from
		// A instead of starting a new one.
		tc.By("converting the subnet to an L1 with a different validator set", func() {
			require.NoError(newValidator.EnsureKeys())
			convertSubnetToL1(tc, pWallet, subnetID, chainID, []l1Validator{
				{node: isolatedA, weight: 2 * isolatedValidatorWeight},
				{node: newValidator, weight: isolatedValidatorWeight},
			})
		})

		// Validator A holds only two thirds of the stake, so it cannot finish syncing the
		// L1 until the new validator is running too.
		tc.By("restarting the isolated nodes and starting the new validator", func() {
			// B keeps running, but it is no longer a validator.
			restartWithoutIsolatedL1(tc, network, subnetID, isolatedA, isolatedB)

			newValidator.Flags[config.TrackSubnetsKey] = subnetID.String()
			newValidator.Flags[config.PartialSyncPrimaryNetworkKey] = "true"
			e2e.AddEphemeralNode(tc, network, newValidator)

			for _, node := range []*tmpnet.Node{isolatedA, isolatedB, newValidator} {
				e2e.WaitForHealthy(tc, node)
			}
		})

		tc.By("syncing the isolated history on the new validator", func() {
			requireReceivedTransfer(tc, newValidator, chainID, isolatedRecipient)
		})

		tc.By("producing blocks with the new validator set", func() {
			recipient := e2e.NewPrivateKey(tc).Address()
			issueTransfer(tc, isolatedA, chainID, chainKey, recipient)
			requireReceivedTransfer(tc, newValidator, chainID, recipient)
		})
	})
})

// createSubnetAndChain issues a CreateSubnetTx and a CreateChainTx for an xsvm
// chain funding [chainKey]. It returns the subnet ID and the CreateChainTx.
func createSubnetAndChain(
	tc *e2e.GinkgoTestContext,
	pWallet pwallet.Wallet,
	keychain *secp256k1fx.Keychain,
	chainKey *secp256k1.PrivateKey,
) (ids.ID, *platform.Tx) {
	require := require.New(tc)

	var (
		subnetID ids.ID
		chainTx  *platform.Tx
	)
	tc.By("creating the subnet and chain on the P-chain", func() {
		owner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keychain.Keys[0].Address()},
		}
		subnetTx, err := pWallet.IssueCreateSubnetTx(owner, tc.WithDefaultContext())
		require.NoError(err)
		subnetID = subnetTx.ID()

		genesisBytes, err := subnet.NewXSVMGenesis(chainKey)
		require.NoError(err)
		chainTx, err = pWallet.IssueCreateChainTx(
			subnetID,
			genesisBytes,
			constants.XSVMID,
			nil,
			"isolated",
			tc.WithDefaultContext(),
		)
		require.NoError(err)
	})
	return subnetID, chainTx
}

// startIsolatedValidators starts [a] and [b] as the isolated validators of the
// chain created by [chainTx] and waits for them to be healthy. A has no peers
// to start from and B starts from A, so neither connects to the P-chain nodes.
func startIsolatedValidators(
	tc *e2e.GinkgoTestContext,
	network *tmpnet.Network,
	chainTx *platform.Tx,
	a, b *tmpnet.Node,
) {
	configFile := writeIsolatedL1ConfigFile(tc, network, chainTx, []*tmpnet.Node{a, b})
	a.Flags[config.IsolatedL1ConfigFileKey] = configFile
	b.Flags[config.IsolatedL1ConfigFileKey] = configFile

	a.Flags[config.BootstrapIPsKey] = ""
	a.Flags[config.BootstrapIDsKey] = ""
	e2e.AddEphemeralNode(tc, network, a)

	b.Flags[config.BootstrapIPsKey] = a.StakingAddress.String()
	b.Flags[config.BootstrapIDsKey] = a.NodeID.String()
	e2e.AddEphemeralNode(tc, network, b)

	e2e.WaitForHealthy(tc, a)
	e2e.WaitForHealthy(tc, b)
}

type l1Validator struct {
	node   *tmpnet.Node
	weight uint64
}

// convertSubnetToL1 converts [subnetID] to an L1 validated by [validators].
func convertSubnetToL1(
	tc *e2e.GinkgoTestContext,
	pWallet pwallet.Wallet,
	subnetID ids.ID,
	chainID ids.ID,
	validators []l1Validator,
) {
	convertValidators := make([]*platform.ConvertSubnetToL1Validator, len(validators))
	for i, vdr := range validators {
		pop, err := vdr.node.GetProofOfPossession()
		require.NoError(tc, err)
		convertValidators[i] = &platform.ConvertSubnetToL1Validator{
			NodeID:  vdr.node.NodeID.Bytes(),
			Weight:  vdr.weight,
			Balance: units.Avax,
			Signer:  *pop,
		}
	}
	_, err := pWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		[]byte{},
		convertValidators,
		tc.WithDefaultContext(),
	)
	require.NoError(tc, err)
}

// restartWithoutIsolatedL1 restarts [nodes] as normal nodes that sync the
// P-chain and track [subnetID]. It does not wait for them to be healthy,
// because a node only finishes syncing the L1 once it is connected to most of
// the L1's stake, which may require other validators to start first.
func restartWithoutIsolatedL1(
	tc *e2e.GinkgoTestContext,
	network *tmpnet.Network,
	subnetID ids.ID,
	nodes ...*tmpnet.Node,
) {
	bootstrapIPs, bootstrapIDs := network.GetBootstrapIPsAndIDs(nil)
	for _, node := range nodes {
		delete(node.Flags, config.IsolatedL1ConfigFileKey)
		node.Flags[config.TrackSubnetsKey] = subnetID.String()
		node.Flags[config.PartialSyncPrimaryNetworkKey] = "true"
		node.Flags[config.BootstrapIPsKey] = strings.Join(bootstrapIPs, ",")
		node.Flags[config.BootstrapIDsKey] = strings.Join(bootstrapIDs, ",")
		require.NoError(tc, node.Restart(tc.DefaultContext()))
	}
}

// requireSignsMessage requires [node] to sign the warp message of the export
// tx [txID] with its BLS key.
func requireSignsMessage(tc *e2e.GinkgoTestContext, node *tmpnet.Node, chainID ids.ID, txID ids.ID) {
	require := require.New(tc)

	msg, sig, err := api.NewClient(node.GetAccessibleURI(), chainID.String()).Message(tc.DefaultContext(), txID)
	require.NoError(err)

	pop, err := node.GetProofOfPossession()
	require.NoError(err)
	pk, err := bls.PublicKeyFromCompressedBytes(pop.PublicKey[:])
	require.NoError(err)
	parsedSig, err := bls.SignatureFromBytes(sig)
	require.NoError(err)
	require.True(bls.Verify(pk, parsedSig, msg.Bytes()))
}

// writeIsolatedL1ConfigFile writes the isolated L1 config for the chain created
// by [tx] and validated by [nodes], and returns the file path.
func writeIsolatedL1ConfigFile(
	tc *e2e.GinkgoTestContext,
	network *tmpnet.Network,
	tx *platform.Tx,
	nodes []*tmpnet.Node,
) string {
	txHex, err := formatting.Encode(formatting.Hex, tx.Bytes())
	require.NoError(tc, err)

	isolatedL1Config := config.IsolatedL1Config{
		CreateChainTx: txHex,
		ChainID:       tx.ID(),
	}
	for _, node := range nodes {
		require.NoError(tc, node.EnsureKeys())
		pop, err := node.GetProofOfPossession()
		require.NoError(tc, err)
		isolatedL1Config.Validators = append(isolatedL1Config.Validators, config.IsolatedL1Validator{
			NodeID: node.NodeID,
			Weight: isolatedValidatorWeight,
			Signer: pop,
		})
	}

	configBytes, err := json.Marshal(isolatedL1Config)
	require.NoError(tc, err)
	path := filepath.Join(network.Dir, fmt.Sprintf("isolated-l1-%s.json", tx.ID()))
	require.NoError(tc, os.WriteFile(path, configBytes, 0o600))
	return path
}

// issueTransfer sends one Schmeckle to [to] through [node] and waits for
// [node] to accept it.
func issueTransfer(tc *e2e.GinkgoTestContext, node *tmpnet.Node, chainID ids.ID, key *secp256k1.PrivateKey, to ids.ShortID) {
	_, err := transfer.Transfer(tc.DefaultContext(), &transfer.Config{
		URI:               node.GetAccessibleURI(),
		ChainID:           chainID,
		AssetID:           chainID,
		Amount:            units.Schmeckle,
		To:                to,
		PrivateKey:        key,
		WaitForAcceptance: true,
	})
	require.NoError(tc, err)
}

// requireReceivedTransfer waits until [node] reports that [addr] holds the
// amount sent by issueTransfer.
func requireReceivedTransfer(tc *e2e.GinkgoTestContext, node *tmpnet.Node, chainID ids.ID, addr ids.ShortID) {
	client := api.NewClient(node.GetAccessibleURI(), chainID.String())
	tc.Eventually(func() bool {
		balance, err := client.Balance(tc.DefaultContext(), addr, chainID)
		return err == nil && balance == units.Schmeckle
	}, tests.DefaultTimeout, e2e.DefaultPollingInterval, "transfer not received")
}
