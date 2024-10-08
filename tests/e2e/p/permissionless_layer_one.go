// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"
	"math"
	"slices"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	p2pmessage "github.com/ava-labs/avalanchego/message"
	p2psdk "github.com/ava-labs/avalanchego/network/p2p"
	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	snowvalidators "github.com/ava-labs/avalanchego/snow/validators"
	platformvmvalidators "github.com/ava-labs/avalanchego/vms/platformvm/validators"
	warpmessage "github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
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

		tc.By("connecting to the genesis validator")
		var (
			networkID           = env.GetNetwork().GetNetworkID()
			genesisPeerMessages = buffer.NewUnboundedBlockingDeque[p2pmessage.InboundMessage](1)
		)
		genesisPeer, err := peer.StartTestPeer(
			tc.DefaultContext(),
			subnetGenesisNode.StakingAddress,
			networkID,
			router.InboundHandlerFunc(func(_ context.Context, m p2pmessage.InboundMessage) {
				tc.Outf("received %s %s from %s", m.Op(), m.Message(), m.NodeID())
				genesisPeerMessages.PushRight(m)
			}),
		)
		require.NoError(err)
		defer func() {
			genesisPeer.StartClose()
			require.NoError(genesisPeer.AwaitClosed(tc.DefaultContext()))
		}()

		const genesisWeight = 100
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

		expectedConversionID, err := warpmessage.SubnetConversionID(warpmessage.SubnetConversionData{
			SubnetID:       subnetID,
			ManagerChainID: chainID,
			ManagerAddress: address,
			Validators: []warpmessage.SubnetConversionValidatorData{
				{
					NodeID:       subnetGenesisNode.NodeID.Bytes(),
					BLSPublicKey: genesisNodePoP.PublicKey,
					Weight:       genesisWeight,
				},
			},
		})
		require.NoError(err)

		tc.By("waiting to update the proposervm P-chain height")
		time.Sleep((5 * platformvmvalidators.RecentlyAcceptedWindowTTL) / 4)

		tc.By("issuing random transactions to update the proposervm P-chain height")
		for range 2 {
			_, err = pWallet.IssueCreateSubnetTx(
				owner,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		}

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
			map[ids.NodeID]*snowvalidators.GetValidatorOutput{
				subnetGenesisNode.NodeID: {
					NodeID:    subnetGenesisNode.NodeID,
					PublicKey: genesisNodePK,
					Weight:    genesisWeight,
				},
			},
			subnetValidators,
		)

		tc.By("creating the validator to register")
		subnetRegisterNode := e2e.AddEphemeralNode(tc, env.GetNetwork(), tmpnet.FlagsMap{
			config.TrackSubnetsKey: subnetID.String(),
		})

		registerNodePoP, err := subnetRegisterNode.GetProofOfPossession()
		require.NoError(err)

		registerNodePK, err := bls.PublicKeyFromCompressedBytes(registerNodePoP.PublicKey[:])
		require.NoError(err)

		tc.By("ensures the subnet nodes are healthy")
		e2e.WaitForHealthy(tc, subnetGenesisNode)
		e2e.WaitForHealthy(tc, subnetRegisterNode)

		const registerWeight = 1
		tc.By("create the unsigned warp message to register the validator")
		unsignedRegisterSubnetValidator := must[*warp.UnsignedMessage](tc)(warp.NewUnsignedMessage(
			networkID,
			chainID,
			must[*payload.AddressedCall](tc)(payload.NewAddressedCall(
				address,
				must[*warpmessage.RegisterSubnetValidator](tc)(warpmessage.NewRegisterSubnetValidator(
					subnetID,
					subnetRegisterNode.NodeID,
					registerNodePoP.PublicKey,
					uint64(time.Now().Add(5*time.Minute).Unix()),
					warpmessage.PChainOwner{},
					warpmessage.PChainOwner{},
					registerWeight, // weight
				)).Bytes(),
			)).Bytes(),
		))

		registerSubnetValidatorRequest, err := wrapWarpSignatureRequest(
			chainID,
			unsignedRegisterSubnetValidator,
			nil,
		)
		require.NoError(err)

		tc.By("send the request to sign the warp message")
		require.True(genesisPeer.Send(tc.DefaultContext(), registerSubnetValidatorRequest))

		tc.By("get the signature response")
		registerSubnetValidatorSignature, ok := findMessage(genesisPeerMessages, unwrapWarpSignature)
		require.True(ok)

		tc.By("create the signed warp message to register the validator")
		signers := set.NewBits()
		signers.Add(0) // [signers] has weight from the genesis peer

		var sigBytes [bls.SignatureLen]byte
		copy(sigBytes[:], bls.SignatureToBytes(registerSubnetValidatorSignature))
		registerSubnetValidator, err := warp.NewMessage(
			unsignedRegisterSubnetValidator,
			&warp.BitSetSignature{
				Signers:   signers.Bytes(),
				Signature: sigBytes,
			},
		)
		require.NoError(err)

		tc.By("register the validator")
		_, err = pWallet.IssueRegisterSubnetValidatorTx(
			1,
			registerNodePoP.ProofOfPossession,
			registerSubnetValidator.Bytes(),
		)
		require.NoError(err)

		tc.By("verify that the validator was registered")
		height, err = pClient.GetHeight(tc.DefaultContext())
		require.NoError(err)

		subnetValidators, err = pClient.GetValidatorsAt(tc.DefaultContext(), subnetID, height)
		require.NoError(err)
		require.Equal(
			map[ids.NodeID]*snowvalidators.GetValidatorOutput{
				subnetGenesisNode.NodeID: {
					NodeID:    subnetGenesisNode.NodeID,
					PublicKey: genesisNodePK,
					Weight:    genesisWeight,
				},
				subnetRegisterNode.NodeID: {
					NodeID:    subnetRegisterNode.NodeID,
					PublicKey: registerNodePK,
					Weight:    registerWeight,
				},
			},
			subnetValidators,
		)
	})
})

func wrapWarpSignatureRequest(
	chainID ids.ID,
	msg *warp.UnsignedMessage,
	justification []byte,
) (p2pmessage.OutboundMessage, error) {
	p2pMessageFactory, err := p2pmessage.NewCreator(
		logging.NoLog{},
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	if err != nil {
		return nil, err
	}

	request := sdk.SignatureRequest{
		Message:       msg.Bytes(),
		Justification: justification,
	}
	requestBytes, err := proto.Marshal(&request)
	if err != nil {
		return nil, err
	}

	return p2pMessageFactory.AppRequest(
		chainID,
		0,
		time.Hour,
		p2psdk.PrefixMessage(
			p2psdk.ProtocolPrefix(p2psdk.SignatureRequestHandlerID),
			requestBytes,
		),
	)
}

func findMessage[T any](
	q buffer.BlockingDeque[p2pmessage.InboundMessage],
	parser func(p2pmessage.InboundMessage) (T, bool),
) (T, bool) {
	var messagesToReprocess []p2pmessage.InboundMessage
	defer func() {
		slices.Reverse(messagesToReprocess)
		for _, msg := range messagesToReprocess {
			q.PushLeft(msg)
		}
	}()

	for {
		msg, ok := q.PopLeft()
		if !ok {
			return utils.Zero[T](), false
		}

		parsed, ok := parser(msg)
		if ok {
			return parsed, true
		}

		messagesToReprocess = append(messagesToReprocess, msg)
	}
}

func unwrapWarpSignature(msg p2pmessage.InboundMessage) (*bls.Signature, bool) {
	appResponse, ok := msg.Message().(*p2ppb.AppResponse)
	if !ok {
		return nil, false
	}

	var response sdk.SignatureResponse
	err := proto.Unmarshal(appResponse.AppBytes, &response)
	if err != nil {
		return nil, false
	}

	warpSignature, err := bls.SignatureFromBytes(response.Signature)
	if err != nil {
		return nil, false
	}
	return warpSignature, true
}

func must[T any](t require.TestingT) func(T, error) T {
	return func(val T, err error) T {
		require.NoError(t, err)
		return val
	}
}
