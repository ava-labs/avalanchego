// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interchain

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ = e2e.DescribeCChain("[Interchain Bootstrap]", func() {
	var (
		tc      = e2e.NewTestContext()
		require = require.New(tc)
	)

	ginkgo.It("bootstraps while the P-Chain and C-Chain are advancing", func() {
		var (
			env       = e2e.GetEnv(tc)
			network   = env.GetNetwork()
			nodeURI   = env.GetRandomNodeURI()
			ctx       = tc.DefaultContext()
			ethClient = e2e.NewEthClient(tc, nodeURI)
		)

		tc.By("initializing clients for both chains")
		var (
			keychain      = env.NewKeychain()
			pWallet       = e2e.NewWallet(tc, keychain, nodeURI).P()
			avaxAssetID   = pWallet.Builder().Context().AVAXAssetID
			rewardAddress = e2e.NewPrivateKey(tc).Address()
			cRecipient    = e2e.NewPrivateKey(tc).EthAddress()
			senderKey     = env.PreFundedKey
			senderAddress = senderKey.EthAddress()
		)
		chainID, err := ethClient.ChainID(ctx)
		require.NoError(err)

		// issuePChainBatch issues and confirms permissionless delegator
		// transactions to advance the P-Chain by the requested number of blocks.
		issuePChainBatch := func(count int) {
			endTime := time.Now().Add(6 * time.Minute)
			for range count {
				_, err := pWallet.IssueAddPermissionlessDelegatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: nodeURI.NodeID,
							End:    uint64(endTime.Unix()),
							Wght:   genesis.LocalParams.StakingConfig.MinDelegatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					avaxAssetID,
					&secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{rewardAddress},
					},
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			}
		}

		// Create the same validator-set updates that preceded the failure in CI.
		// Do not create a C-Chain block yet. The first post-genesis C-Chain block
		// must refer to a P-Chain height that the new node has not reached.
		const initialDelegatorCount = 15
		tc.By("adding an initial batch of P-Chain delegators", func() {
			issuePChainBatch(initialDelegatorCount)
		})
		tc.By("starting a node with both chains behind the network")
		bootstrapNode := tmpnet.NewEphemeralNode(tmpnet.FlagsMap{})
		// StartNode returns when the process has started, not when its chains are
		// healthy.
		require.NoError(network.StartNode(ctx, bootstrapNode))
		tc.DeferCleanup(func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
			defer cancel()
			require.NoError(bootstrapNode.Stop(stopCtx))
		})

		// Wait until the new node's P-Chain has reached its selected bootstrap
		// frontier and is waiting for the C-Chain to finish syncing. Advancing the
		// established network before this point could allow the new node to select
		// the newer P-Chain height as its frontier and avoid the failure.
		const waitingForRemainingChains = "waiting for the remaining chains in this subnet to finish syncing"
		pChainLogPath := filepath.Join(bootstrapNode.DataDir, "logs", "P.log")
		tc.By("waiting for the new P-Chain to reach its bootstrap frontier", func() {
			tc.Eventually(
				func() bool {
					pChainLog, err := os.ReadFile(pChainLogPath)
					return err == nil && strings.Contains(string(pChainLog), waitingForRemainingChains)
				},
				e2e.DefaultTimeout,
				e2e.DefaultPollingInterval,
				"new P-Chain did not reach its bootstrap frontier before timeout",
			)
		})

		// Advance the established network's P-Chain after the new node selected
		// its bootstrap frontier. The subsequent C-Chain proposer block records
		// this newer P-Chain height and is the first C-Chain block the new node
		// must verify.
		tc.By("advancing the P-Chain while the new node waits for the C-Chain", func() {
			issuePChainBatch(1)
		})
		tc.By("confirming a C-Chain transaction at the advanced P-Chain height", func() {
			nonce, err := ethClient.AcceptedNonceAt(ctx, senderAddress)
			require.NoError(err)
			tx := types.NewTransaction(
				nonce,
				cRecipient,
				big.NewInt(1),
				e2e.DefaultGasLimit,
				e2e.SuggestGasPrice(tc, ethClient),
				nil,
			)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), senderKey.ToECDSA())
			require.NoError(err)
			receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
			require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
		})

		// Health is the behavior under test. Because C-Chain is critical, its
		// shutdown also stops the node and causes this check to fail immediately.
		// Otherwise, allow the standard e2e deadline for bootstrap on slow hosts.
		tc.By("waiting for the new node to become healthy")
		healthErr := bootstrapNode.WaitForHealthy(ctx)
		if healthErr == nil {
			return
		}

		// Keep the health failure as the assertion result. Reading C.log only
		// adds the known bootstrap failure to the Ginkgo output for diagnosis.
		const heightMismatch = "block P-chain height larger than current P-chain height"
		cChainLogPath := filepath.Join(bootstrapNode.DataDir, "logs", "C.log")
		cChainLog, logErr := os.ReadFile(cChainLogPath)
		if logErr != nil {
			require.NoError(
				healthErr,
				"C-Chain log: %s (failed to read log: %v)",
				cChainLogPath,
				logErr,
			)
			return
		}

		var fatalLine string
		lines := strings.Split(string(cChainLog), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.Contains(lines[i], "FATAL <C Chain>") && strings.Contains(lines[i], heightMismatch) {
				fatalLine = lines[i]
				break
			}
		}
		if fatalLine == "" {
			require.NoError(
				healthErr,
				"C-Chain log does not contain the expected height mismatch: %s",
				cChainLogPath,
			)
			return
		}
		require.NoError(
			healthErr,
			"C-Chain bootstrap failure: %s\nC-Chain log: %s",
			fatalLine,
			cChainLogPath,
		)
	})
})
