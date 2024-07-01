// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"fmt"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = e2e.DescribePChain("[Validator Sets]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("should be identical for every height for all nodes in the network", func() {
		network := e2e.Env.GetNetwork()

		ginkgo.By("creating wallet with a funded key to source delegated funds from")
		keychain := e2e.Env.NewKeychain()
		nodeURI := e2e.Env.GetRandomNodeURI()
		baseWallet := e2e.NewWallet(keychain, nodeURI)
		pWallet := baseWallet.P()

		pBuilder := pWallet.Builder()
		pContext := pBuilder.Context()

		const delegatorCount = 15
		ginkgo.By(fmt.Sprintf("adding %d delegators", delegatorCount), func() {
			rewardKey, err := secp256k1.NewPrivateKey()
			require.NoError(err)
			avaxAssetID := pContext.AVAXAssetID
			startTime := time.Now().Add(tmpnet.DefaultValidatorStartTimeDiff)
			endTime := startTime.Add(time.Second * 360)
			// This is the default flag value for MinDelegatorStake.
			weight := genesis.LocalParams.StakingConfig.MinDelegatorStake

			for i := 0; i < delegatorCount; i++ {
				_, err = pWallet.IssueAddPermissionlessDelegatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: nodeURI.NodeID,
							Start:  uint64(startTime.Unix()),
							End:    uint64(endTime.Unix()),
							Wght:   weight,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					avaxAssetID,
					&secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{rewardKey.Address()},
					},
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			}
		})

		ginkgo.By("getting the current P-Chain height from the wallet")
		currentPChainHeight, err := platformvm.NewClient(nodeURI.URI).GetHeight(e2e.DefaultContext())
		require.NoError(err)

		ginkgo.By("checking that validator sets are equal across all heights for all nodes", func() {
			pvmClients := make([]platformvm.Client, len(e2e.Env.URIs))
			for i, nodeURI := range e2e.Env.URIs {
				pvmClients[i] = platformvm.NewClient(nodeURI.URI)
				// Ensure that the height of the target node is at least the expected height
				e2e.Eventually(
					func() bool {
						pChainHeight, err := pvmClients[i].GetHeight(e2e.DefaultContext())
						require.NoError(err)
						return pChainHeight >= currentPChainHeight
					},
					e2e.DefaultTimeout,
					e2e.DefaultPollingInterval,
					fmt.Sprintf("failed to see expected height %d for %s before timeout", currentPChainHeight, nodeURI.NodeID),
				)
			}

			for height := uint64(0); height <= currentPChainHeight; height++ {
				tests.Outf(" checked validator sets for height %d\n", height)
				var observedValidatorSet map[ids.NodeID]*validators.GetValidatorOutput
				for _, pvmClient := range pvmClients {
					validatorSet, err := pvmClient.GetValidatorsAt(
						e2e.DefaultContext(),
						constants.PrimaryNetworkID,
						height,
					)
					require.NoError(err)
					if observedValidatorSet == nil {
						observedValidatorSet = validatorSet
						continue
					}
					require.Equal(observedValidatorSet, validatorSet)
				}
			}
		})

		e2e.CheckBootstrapIsPossible(network)
	})
})
