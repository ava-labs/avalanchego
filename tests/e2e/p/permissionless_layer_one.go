// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"math"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const genesisWeight = 100

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

		genesisKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)

		genesisBytes, err := genesis.Codec.Marshal(genesis.CodecVersion, &genesis.Genesis{
			Timestamp: time.Now().Unix(),
			Allocations: []genesis.Allocation{
				{
					Address: genesisKey.Address(),
					Balance: math.MaxUint64,
				},
			},
		})
		require.NoError(err)

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

		tc.By("issuing a CreateChainTx")
		chainTx, err := pWallet.IssueCreateChainTx(
			subnetID,
			genesisBytes,
			constants.XSVMID,
			nil,
			"No Permissions",
			tc.WithDefaultContext(),
		)
		require.NoError(err)

		tc.By("creating the genesis validator")
		subnetGenesisNode := e2e.AddEphemeralNode(tc, env.GetNetwork(), tmpnet.FlagsMap{
			config.TrackSubnetsKey: subnetID.String(),
		})

		genesisNodePoP, err := subnetGenesisNode.GetProofOfPossession()
		require.NoError(err)

		genesisNodePK, err := bls.PublicKeyFromCompressedBytes(genesisNodePoP.PublicKey[:])
		require.NoError(err)

		var (
			chainID = chainTx.ID()
			address = []byte{}
		)
		tc.By("issuing a ConvertSubnetTx")
		_, err = pWallet.IssueConvertSubnetTx(
			subnetID,
			chainID,
			address,
			[]*txs.ConvertSubnetValidator{
				{
					NodeID:  subnetGenesisNode.NodeID.Bytes(),
					Weight:  genesisWeight,
					Balance: units.Avax,
					Signer:  *genesisNodePoP,
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
					NodeID:       subnetGenesisNode.NodeID.Bytes(),
					BLSPublicKey: genesisNodePoP.PublicKey,
					Weight:       genesisWeight,
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

		tc.By("verifying the Permissionless L1 reports the correct validator set")
		height, err := pClient.GetHeight(tc.DefaultContext())
		require.NoError(err)

		subnetValidators, err := pClient.GetValidatorsAt(tc.DefaultContext(), subnetID, height)
		require.NoError(err)
		require.Equal(
			map[ids.NodeID]*validators.GetValidatorOutput{
				subnetGenesisNode.NodeID: {
					NodeID:    subnetGenesisNode.NodeID,
					PublicKey: genesisNodePK,
					Weight:    genesisWeight,
				},
			},
			subnetValidators,
		)
	})
})
