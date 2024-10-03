// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ = e2e.DescribePChain("[Permissionless L1]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("creates a Permissionless L1", func() {
		env := e2e.GetEnv(tc)
		nodeURI := env.GetRandomNodeURI()
		infoClient := info.NewClient(nodeURI.URI)

		tc.By("fetching upgrade config")
		upgrades, err := infoClient.Upgrades(tc.DefaultContext())
		require.NoError(err)

		tc.By("verifying Etna is activated")
		now := time.Now()
		if !upgrades.IsEtnaActivated(now) {
			ginkgo.Skip("Etna is not activated. Permissionless L1s are enabled post-Etna, skipping test.")
		}

		keychain := env.NewKeychain()
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)

		pWallet := baseWallet.P()
		pClient := platformvm.NewClient(nodeURI.URI)

		owner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				keychain.Keys[0].Address(),
			},
		}

		tc.By("issuing a CreateSubnetTx")
		subnetTx, err := pWallet.IssueCreateSubnetTx(
			owner,
			tc.WithDefaultContext(),
		)
		require.NoError(err)

		tc.By("verifying a Permissioned Subnet was successfully created")
		subnetID := subnetTx.ID()
		require.NotEqual(subnetID, constants.PrimaryNetworkID)

		res, err := pClient.GetSubnet(tc.DefaultContext(), subnetID)
		require.NoError(err)

		require.Equal(
			platformvm.GetSubnetClientResponse{
				IsPermissioned: true,
				ControlKeys: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
				Threshold: 1,
			},
			res,
		)

		const weight = 100
		var (
			chainID = ids.GenerateTestID()
			address = []byte{'a', 'd', 'd', 'r', 'e', 's', 's'}
			nodeID  = ids.GenerateTestNodeID()
		)

		sk, err := bls.NewSecretKey()
		require.NoError(err)
		pop := signer.NewProofOfPossession(sk)

		tc.By("issuing a ConvertSubnetTx")
		_, err = pWallet.IssueConvertSubnetTx(
			subnetID,
			chainID,
			address,
			[]*txs.ConvertSubnetValidator{
				{
					NodeID: nodeID.Bytes(),
					Weight: weight,
					Signer: *pop,
				},
			},
			tc.WithDefaultContext(),
		)
		require.NoError(err)

		expectedConversionID, err := message.SubnetConversionID(message.SubnetConversionData{
			SubnetID:       subnetID,
			ManagerChainID: chainID,
			ManagerAddress: address,
			Validators: []message.SubnetConversionValidatorData{
				{
					NodeID:       nodeID.Bytes(),
					BLSPublicKey: pop.PublicKey,
					Weight:       weight,
				},
			},
		})
		require.NoError(err)

		tc.By("verifying the Permissioned Subnet was converted to a Permissionless L1")
		res, err = pClient.GetSubnet(tc.DefaultContext(), subnetID)
		require.NoError(err)

		require.Equal(
			platformvm.GetSubnetClientResponse{
				IsPermissioned: false,
				ControlKeys: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
				Threshold:      1,
				ConversionID:   expectedConversionID,
				ManagerChainID: chainID,
				ManagerAddress: address,
			},
			res,
		)
	})
})
