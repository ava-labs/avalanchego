// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func requireHeliconActivated(
	tc *e2e.GinkgoTestContext,
	assertions *require.Assertions,
	infoClient *info.Client,
) {
	upgrades, err := infoClient.Upgrades(tc.DefaultContext())
	assertions.NoError(err)

	if upgrades.HeliconTime.Equal(upgrade.UnscheduledActivationTime) {
		ginkgo.Skip("skipping test because Helicon isn't scheduled")
	}

	if wait := time.Until(upgrades.HeliconTime); wait > 0 {
		tc.By("waiting for Helicon activation", func() {
			time.Sleep(wait)
		})
	}

	tc.Eventually(func() bool {
		upgrades, err = infoClient.Upgrades(tc.DefaultContext())
		assertions.NoError(err)

		return upgrades.IsHeliconActivated(time.Now())
	}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "Helicon should have activated")
}

// balanceOf returns the AVAX balance of key.
func balanceOf(
	tc *e2e.GinkgoTestContext,
	require *require.Assertions,
	walletNodeURI tmpnet.NodeURI,
	key *secp256k1.PrivateKey,
) uint64 {
	wallet := e2e.NewWallet(tc, secp256k1fx.NewKeychain(key), walletNodeURI).P()
	balances, err := wallet.Builder().GetBalance()
	require.NoError(err)
	return balances[wallet.Builder().Context().AVAXAssetID]
}

// currentSupply returns the primary network's current supply.
func currentSupply(tc *e2e.GinkgoTestContext, require *require.Assertions, pvmClient *platformvm.Client) uint64 {
	supply, _, err := pvmClient.GetCurrentSupply(tc.DefaultContext(), constants.PrimaryNetworkID)
	require.NoError(err)
	return supply
}
