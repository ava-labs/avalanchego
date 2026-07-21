// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/proposervm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	p2pmessage "github.com/ava-labs/avalanchego/message"
	p2psdk "github.com/ava-labs/avalanchego/network/p2p"
	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	platformvmpb "github.com/ava-labs/avalanchego/proto/pb/platformvm"
	snowvalidators "github.com/ava-labs/avalanchego/snow/validators"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	platformvmvalidators "github.com/ava-labs/avalanchego/vms/platformvm/validators"
	warpmessage "github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	pwallet "github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
)

const (
	genesisWeight   = units.Schmeckle
	genesisBalance  = units.Avax
	registerWeight  = genesisWeight / 10
	updatedWeight   = 2 * registerWeight
	registerBalance = 0

	// Validator registration attempts expire 5 minutes after they are created
	expiryDelay = 5 * time.Minute
	// P2P message requests timeout after 10 seconds
	p2pTimeout = 10 * time.Second

	timeToAdvancePChainWindow = 5 * platformvmvalidators.RecentlyAcceptedWindowTTL / 4
)

var _ = e2e.DescribePChain("[L1]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("creates and updates L1 validators", func() {
		env := e2e.GetEnv(tc)
		nodeURI := env.GetRandomNodeURI()

		tc.By("loading the wallet")
		var (
			keychain       = env.NewKeychain()
			baseWallet     = e2e.NewWallet(tc, keychain, nodeURI)
			pWallet        = baseWallet.P()
			pClient        = platformvm.NewClient(nodeURI.URI)
			proposerClient = proposervm.NewJSONRPCClient(nodeURI.URI, "P")
			infoClient     = info.NewClient(nodeURI.URI)
			owner          = &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
			}
		)

		tc.By("creating the chain genesis")
		genesisBytes := newXSVMGenesisBytes(tc)

		var subnetID ids.ID
		tc.By("issuing a CreateSubnetTx", func() {
			subnetTx, err := pWallet.IssueCreateSubnetTx(
				owner,
				tc.WithDefaultContext(),
			)
			require.NoError(err)

			subnetID = subnetTx.ID()
		})

		tc.By("verifying a Permissioned Subnet was successfully created", func() {
			require.NotEqual(constants.PrimaryNetworkID, subnetID)

			subnet, err := pClient.GetSubnet(tc.DefaultContext(), subnetID)
			require.NoError(err)
			require.Equal(
				platformvm.GetSubnetClientResponse{
					IsPermissioned: true,
					ControlKeys: []ids.ShortID{
						keychain.Keys[0].Address(),
					},
					Threshold: 1,
				},
				subnet,
			)
		})

		var chainID ids.ID
		tc.By("issuing a CreateChainTx", func() {
			chainTx, err := pWallet.IssueCreateChainTx(
				subnetID,
				genesisBytes,
				constants.XSVMID,
				nil,
				"No Permissions",
				tc.WithDefaultContext(),
			)
			require.NoError(err)

			chainID = chainTx.ID()
		})

		tc.By("verifying the Permissioned Subnet is configured as expected", func() {
			tc.By("verifying the subnet reports as permissioned", func() {
				subnet, err := pClient.GetSubnet(tc.DefaultContext(), subnetID)
				require.NoError(err)
				require.Equal(
					platformvm.GetSubnetClientResponse{
						IsPermissioned: true,
						ControlKeys: []ids.ShortID{
							keychain.Keys[0].Address(),
						},
						Threshold: 1,
					},
					subnet,
				)
			})

			tc.By("verifying the validator set is empty", func() {
				verifyValidatorSet(tc, pClient, subnetID, map[ids.NodeID]*snowvalidators.GetValidatorOutput{})
			})
		})

		tc.By("creating the genesis validator")
		subnetGenesisNode := e2e.AddEphemeralNode(tc, env.GetNetwork(), tmpnet.NewEphemeralNode(tmpnet.FlagsMap{
			config.TrackSubnetsKey: subnetID.String(),
		}))

		genesisNodePoP, genesisNodePK := nodeProofOfPossession(tc, subnetGenesisNode)

		tc.By("connecting to the genesis validator")
		networkID := env.GetNetwork().GetNetworkID()
		genesisPeer, genesisPeerMessages := startL1TestPeer(tc, subnetGenesisNode, networkID)

		subnetGenesisNodeURI := subnetGenesisNode.GetAccessibleURI()

		address := []byte{}
		tc.By("issuing a ConvertSubnetToL1Tx", func() {
			tx, err := pWallet.IssueConvertSubnetToL1Tx(
				subnetID,
				chainID,
				address,
				[]*txs.ConvertSubnetToL1Validator{
					{
						NodeID:  subnetGenesisNode.NodeID.Bytes(),
						Weight:  genesisWeight,
						Balance: genesisBalance,
						Signer:  *genesisNodePoP,
					},
				},
				tc.WithDefaultContext(),
			)
			require.NoError(err)

			awaitTxAccepted(tc, subnetGenesisNodeURI, tx.ID())
		})
		genesisValidationID := subnetID.Append(0)

		tc.By("verifying the Permissioned Subnet was converted to an L1", func() {
			expectedConversionID, err := warpmessage.SubnetToL1ConversionID(warpmessage.SubnetToL1ConversionData{
				SubnetID:       subnetID,
				ManagerChainID: chainID,
				ManagerAddress: address,
				Validators: []warpmessage.SubnetToL1ConversionValidatorData{
					{
						NodeID:       subnetGenesisNode.NodeID.Bytes(),
						BLSPublicKey: genesisNodePoP.PublicKey,
						Weight:       genesisWeight,
					},
				},
			})
			require.NoError(err)

			tc.By("verifying the subnet reports as being converted", func() {
				subnet, err := pClient.GetSubnet(tc.DefaultContext(), subnetID)
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
					subnet,
				)
			})

			tc.By("verifying the validator set was updated", func() {
				verifyValidatorSet(tc, pClient, subnetID, map[ids.NodeID]*snowvalidators.GetValidatorOutput{
					subnetGenesisNode.NodeID: {
						NodeID:    subnetGenesisNode.NodeID,
						PublicKey: genesisNodePK,
						Weight:    genesisWeight,
					},
				})
			})

			tc.By("verifying the L1 validator can be fetched", func() {
				l1Validator, _, err := pClient.GetL1Validator(tc.DefaultContext(), genesisValidationID)
				require.NoError(err)
				require.LessOrEqual(l1Validator.Balance, genesisBalance)

				l1Validator.StartTime = 0
				l1Validator.Balance = 0
				require.Equal(
					platformvm.L1Validator{
						SubnetID:  subnetID,
						NodeID:    subnetGenesisNode.NodeID,
						PublicKey: genesisNodePK,
						RemainingBalanceOwner: &secp256k1fx.OutputOwners{
							Addrs: []ids.ShortID{},
						},
						DeactivationOwner: &secp256k1fx.OutputOwners{
							Addrs: []ids.ShortID{},
						},
						Weight:   genesisWeight,
						MinNonce: 0,
					},
					l1Validator,
				)
			})

			tc.By("fetching the subnet conversion attestation", func() {
				conversion, err := warpmessage.NewSubnetToL1Conversion(expectedConversionID)
				require.NoError(err)
				verifyPChainAttestation(tc, genesisPeer, genesisPeerMessages, networkID, genesisNodePK, conversion.Bytes(), subnetID[:])
			})
		})

		tc.By("advancing the proposervm P-chain height", func() {
			advanceProposerVMPChainHeight(tc, pWallet, infoClient, proposerClient, subnetGenesisNodeURI)
		})

		tc.By("creating the validator to register")
		subnetRegisterNode := e2e.AddEphemeralNode(tc, env.GetNetwork(), tmpnet.NewEphemeralNode(tmpnet.FlagsMap{
			config.TrackSubnetsKey: subnetID.String(),
		}))

		registerNodePoP, registerNodePK := nodeProofOfPossession(tc, subnetRegisterNode)

		tc.By("ensuring the subnet nodes are healthy", func() {
			e2e.WaitForHealthy(tc, subnetGenesisNode)
			e2e.WaitForHealthy(tc, subnetRegisterNode)
		})

		tc.By("creating the RegisterL1ValidatorMessage")
		expiry := uint64(time.Now().Add(expiryDelay).Unix()) // This message will expire in 5 minutes
		registerL1ValidatorMessage, err := warpmessage.NewRegisterL1Validator(
			subnetID,
			subnetRegisterNode.NodeID,
			registerNodePoP.PublicKey,
			expiry,
			warpmessage.PChainOwner{},
			warpmessage.PChainOwner{},
			registerWeight,
		)
		require.NoError(err)
		registerValidationID := registerL1ValidatorMessage.ValidationID()
		manager := &l1Manager{
			pWallet:       pWallet,
			genesisPeer:   genesisPeer,
			peerMessages:  genesisPeerMessages,
			networkID:     networkID,
			chainID:       chainID,
			address:       address,
			acceptanceURI: subnetGenesisNodeURI,
		}

		tc.By("registering the validator", func() {
			manager.registerValidator(tc, registerL1ValidatorMessage, registerBalance, registerNodePoP.ProofOfPossession)
		})

		tc.By("verifying the validator was registered", func() {
			tc.By("verifying the validator set was updated", func() {
				verifyValidatorSet(tc, pClient, subnetID, map[ids.NodeID]*snowvalidators.GetValidatorOutput{
					subnetGenesisNode.NodeID: {
						NodeID:    subnetGenesisNode.NodeID,
						PublicKey: genesisNodePK,
						Weight:    genesisWeight,
					},
					ids.EmptyNodeID: { // The validator is not active
						NodeID: ids.EmptyNodeID,
						Weight: registerWeight,
					},
				})
			})

			tc.By("verifying the L1 validator can be fetched", func() {
				l1Validator, _, err := pClient.GetL1Validator(tc.DefaultContext(), registerValidationID)
				require.NoError(err)

				l1Validator.StartTime = 0
				require.Equal(
					platformvm.L1Validator{
						SubnetID:  subnetID,
						NodeID:    subnetRegisterNode.NodeID,
						PublicKey: registerNodePK,
						RemainingBalanceOwner: &secp256k1fx.OutputOwners{
							Addrs: []ids.ShortID{},
						},
						DeactivationOwner: &secp256k1fx.OutputOwners{
							Addrs: []ids.ShortID{},
						},
						Weight:   registerWeight,
						MinNonce: 0,
						Balance:  0,
					},
					l1Validator,
				)
			})

			tc.By("fetching the validator registration attestation", func() {
				registration, err := warpmessage.NewL1ValidatorRegistration(registerValidationID, true)
				require.NoError(err)
				verifyPChainAttestation(tc, genesisPeer, genesisPeerMessages, networkID, genesisNodePK, registration.Bytes(), nil)
			})
		})

		var nextNonce uint64
		setWeight := func(validationID ids.ID, weight uint64) {
			manager.setValidatorWeight(tc, validationID, nextNonce, weight)
			nextNonce++
		}

		tc.By("increasing the weight of the validator", func() {
			setWeight(registerValidationID, updatedWeight)
		})

		tc.By("verifying the validator weight was increased", func() {
			tc.By("verifying the validator set was updated", func() {
				verifyValidatorSet(tc, pClient, subnetID, map[ids.NodeID]*snowvalidators.GetValidatorOutput{
					subnetGenesisNode.NodeID: {
						NodeID:    subnetGenesisNode.NodeID,
						PublicKey: genesisNodePK,
						Weight:    genesisWeight,
					},
					ids.EmptyNodeID: { // The validator is not active
						NodeID: ids.EmptyNodeID,
						Weight: updatedWeight,
					},
				})
			})

			tc.By("verifying the L1 validator can be fetched", func() {
				l1Validator, _, err := pClient.GetL1Validator(tc.DefaultContext(), registerValidationID)
				require.NoError(err)

				l1Validator.StartTime = 0
				require.Equal(
					platformvm.L1Validator{
						SubnetID:  subnetID,
						NodeID:    subnetRegisterNode.NodeID,
						PublicKey: registerNodePK,
						RemainingBalanceOwner: &secp256k1fx.OutputOwners{
							Addrs: []ids.ShortID{},
						},
						DeactivationOwner: &secp256k1fx.OutputOwners{
							Addrs: []ids.ShortID{},
						},
						Weight:   updatedWeight,
						MinNonce: nextNonce,
						Balance:  0,
					},
					l1Validator,
				)
			})

			tc.By("fetching the validator weight change attestation", func() {
				weightMessage, err := warpmessage.NewL1ValidatorWeight(
					registerValidationID,
					nextNonce-1,
					updatedWeight,
				)
				require.NoError(err)
				verifyPChainAttestation(tc, genesisPeer, genesisPeerMessages, networkID, genesisNodePK, weightMessage.Bytes(), nil)
			})
		})

		tc.By("issuing an IncreaseL1ValidatorBalanceTx", func() {
			_, err := pWallet.IssueIncreaseL1ValidatorBalanceTx(
				registerValidationID,
				units.Avax,
			)
			require.NoError(err)
		})

		tc.By("verifying the validator was activated", func() {
			verifyValidatorSet(tc, pClient, subnetID, map[ids.NodeID]*snowvalidators.GetValidatorOutput{
				subnetGenesisNode.NodeID: {
					NodeID:    subnetGenesisNode.NodeID,
					PublicKey: genesisNodePK,
					Weight:    genesisWeight,
				},
				subnetRegisterNode.NodeID: {
					NodeID:    subnetRegisterNode.NodeID,
					PublicKey: registerNodePK,
					Weight:    updatedWeight,
				},
			})
		})

		tc.By("issuing a DisableL1ValidatorTx", func() {
			_, err := pWallet.IssueDisableL1ValidatorTx(
				registerValidationID,
			)
			require.NoError(err)
		})

		tc.By("verifying the validator was deactivated", func() {
			verifyValidatorSet(tc, pClient, subnetID, map[ids.NodeID]*snowvalidators.GetValidatorOutput{
				subnetGenesisNode.NodeID: {
					NodeID:    subnetGenesisNode.NodeID,
					PublicKey: genesisNodePK,
					Weight:    genesisWeight,
				},
				ids.EmptyNodeID: {
					NodeID: ids.EmptyNodeID,
					Weight: updatedWeight,
				},
			})
		})

		tc.By("advancing the proposervm P-chain height", func() {
			advanceProposerVMPChainHeight(tc, pWallet, infoClient, proposerClient, subnetGenesisNodeURI)
		})

		tc.By("removing the registered validator", func() {
			setWeight(registerValidationID, 0)
		})

		tc.By("verifying the validator was removed", func() {
			tc.By("verifying the validator set was updated", func() {
				verifyValidatorSet(tc, pClient, subnetID, map[ids.NodeID]*snowvalidators.GetValidatorOutput{
					subnetGenesisNode.NodeID: {
						NodeID:    subnetGenesisNode.NodeID,
						PublicKey: genesisNodePK,
						Weight:    genesisWeight,
					},
				})
			})

			tc.By("fetching the validator removal attestation", func() {
				registration, err := warpmessage.NewL1ValidatorRegistration(registerValidationID, false)
				require.NoError(err)
				justification := platformvmpb.L1ValidatorRegistrationJustification{
					Preimage: &platformvmpb.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{
						RegisterL1ValidatorMessage: registerL1ValidatorMessage.Bytes(),
					},
				}
				justificationBytes, err := proto.Marshal(&justification)
				require.NoError(err)

				verifyPChainAttestation(tc, genesisPeer, genesisPeerMessages, networkID, genesisNodePK, registration.Bytes(), justificationBytes)
			})
		})

		closeTestPeer(tc, genesisPeer, genesisPeerMessages)

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})

///// Helpers ///////

// newXSVMGenesisBytes returns the Bytes of an XSVM genesis that allocates
// the supply to a new key
func newXSVMGenesisBytes(tc tests.TestContext) []byte {
	require := require.New(tc)

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
	return genesisBytes
}

// nodeProofOfPossession returns the BLS proof of possession and public key of the provided node
func nodeProofOfPossession(tc tests.TestContext, node *tmpnet.Node) (*signer.ProofOfPossession, *bls.PublicKey) {
	require := require.New(tc)
	pop, err := node.GetProofOfPossession()
	require.NoError(err)

	pk, err := bls.PublicKeyFromCompressedBytes(pop.PublicKey[:])
	require.NoError(err)

	return pop, pk
}

// awaitTxAccepted polls the P-Chain API at uri until the transaction has been accepted
func awaitTxAccepted(tc tests.TestContext, uri string, txID ids.ID) {
	tc.By("ensuring the genesis peer has accepted the tx at "+uri, func() {
		client := platformvm.NewClient(uri)
		tc.Eventually(
			func() bool {
				_, err := client.GetTx(tc.DefaultContext(), txID)
				return err == nil
			},
			tests.DefaultTimeout,
			e2e.DefaultPollingInterval,
			"transaction not accepted",
		)
	})
}

// newAddressedCallMessage returns an unsigned warp message from [sourceChainID] whose payload is an AddressedCall from sourceAddress containing payloadBytes
func newAddressedCallMessage(
	tc tests.TestContext,
	networkID uint32,
	sourceChainID ids.ID,
	sourceAddress []byte,
	payloadBytes []byte,
) *warp.UnsignedMessage {
	require := require.New(tc)
	addressedCall, err := payload.NewAddressedCall(sourceAddress, payloadBytes)
	require.NoError(err)

	msg, err := warp.NewUnsignedMessage(networkID, sourceChainID, addressedCall.Bytes())
	require.NoError(err)

	return msg
}

// startL1TestPeer connects a test peer to [node] and returns it with a deque that collects every inbound message
func startL1TestPeer(
	tc tests.TestContext,
	node *tmpnet.Node,
	networkID uint32,
) (*peer.Peer, buffer.BlockingDeque[*p2pmessage.InboundMessage]) {
	require := require.New(tc)
	messages := buffer.NewUnboundedBlockingDeque[*p2pmessage.InboundMessage](1)

	stakingAddress, cancel, err := node.GetAccessibleStakingAddress(tc.DefaultContext())
	require.NoError(err)
	tc.DeferCleanup(cancel)
	p, err := peer.StartTestPeer(
		tc.DefaultContext(),
		stakingAddress,
		networkID,
		router.InboundHandlerFunc(func(_ context.Context, m *p2pmessage.InboundMessage) {
			tc.Log().Info("received a message",
				zap.Stringer("op", m.Op),
				zap.Stringer("message", m.Message),
				zap.Stringer("from", m.NodeID),
			)
			messages.PushRight(m)
		}),
	)
	require.NoError(err)
	return p, messages
}

// closes [p] and its message deque
func closeTestPeer(
	tc tests.TestContext,
	p *peer.Peer,
	messages buffer.BlockingDeque[*p2pmessage.InboundMessage],
) {
	require := require.New(tc)

	messages.Close()
	p.StartClose()
	require.NoError(p.AwaitClosed(tc.DefaultContext()))
}

// requestWarpSignature requests that [p] signs [msg], optionally providing s justification, and returns the resulting BLS signature.
func requestWarpSignature(
	tc tests.TestContext,
	p *peer.Peer,
	messages buffer.BlockingDeque[*p2pmessage.InboundMessage],
	msg *warp.UnsignedMessage,
	justification []byte,
) *bls.Signature {
	require := require.New(tc)

	tc.By("sending the request to sign the warp message", func() {
		request, err := wrapWarpSignatureRequest(msg, justification)
		require.NoError(err)
		require.True(p.Send(tc.DefaultContext(), request))
	})

	var signature *bls.Signature
	tc.By("getting the signature response", func() {
		sig, ok, err := findMessage(messages, unwrapWarpSignature)
		require.NoError(err)
		require.True(ok)
		signature = sig
	})
	return signature
}

// verifyPChainAttestation verifies that the P-Chain attests to [payload] by requesting a signature over it from [p] and verifying the signature against [signerPK]
func verifyPChainAttestation(
	tc tests.TestContext,
	p *peer.Peer,
	messages buffer.BlockingDeque[*p2pmessage.InboundMessage],
	networkID uint32,
	signerPK *bls.PublicKey,
	payloadBytes []byte,
	justification []byte,
) {
	require := require.New(tc)

	unsignedMessage := newAddressedCallMessage(tc, networkID, constants.PlatformChainID, nil, payloadBytes)
	signature := requestWarpSignature(tc, p, messages, unsignedMessage, justification)
	require.True(bls.Verify(signerPK, signature, unsignedMessage.Bytes()))
}

// verifyValidatorSet checks that the subnet validator set at the current heigt matches [expectedValidators]
func verifyValidatorSet(
	tc tests.TestContext,
	pClient *platformvm.Client,
	subnetID ids.ID,
	expectedValidators map[ids.NodeID]*snowvalidators.GetValidatorOutput,
) {
	require := require.New(tc)

	height, err := pClient.GetHeight(tc.DefaultContext())
	require.NoError(err)

	subnetValidators, err := pClient.GetValidatorsAt(tc.DefaultContext(), subnetID, platformapi.Height(height))
	require.NoError(err)
	require.Equal(expectedValidators, subnetValidators)

	flattenedExpectedValidators, err := snowvalidators.FlattenValidatorSet(expectedValidators)
	require.NoError(err)

	if len(flattenedExpectedValidators.Validators) == 0 {
		flattenedExpectedValidators.Validators = nil
	}

	allValidators, err := pClient.GetAllValidatorsAt(tc.DefaultContext(), platformapi.Height(height))
	require.NoError(err)
	require.Equal(flattenedExpectedValidators, allValidators[subnetID])
}

// advanceProposerVMPChainHeight ensures that the proposervm's referenced P-Chain height advances past the current height and advancing the epoch by issuing a dummy transaction.
func advanceProposerVMPChainHeight(
	tc tests.TestContext,
	pWallet pwallet.Wallet,
	infoClient *info.Client,
	proposerClient *proposervm.JSONRPCClient,
	acceptanceURI string,
) {
	require := require.New(tc)

	upgrades, err := infoClient.Upgrades(tc.DefaultContext())
	require.NoError(err)

	if !upgrades.IsGraniteActivated(time.Now()) {
		time.Sleep(timeToAdvancePChainWindow)
		return
	}
	epochBefore, err := proposerClient.GetCurrentEpoch(tc.DefaultContext())
	require.NoError(err)

	tc.By("waiting", func() {
		timeToAdvanceEpoch := max(timeToAdvancePChainWindow, upgrades.GraniteEpochDuration)
		time.Sleep(timeToAdvanceEpoch)
	})

	tc.By("Issuing a dummy tx to advance the epoch", func() {
		tx, err := pWallet.IssueBaseTx(
			[]*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: pWallet.Builder().Context().AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 100 * units.MicroAvax,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs: []ids.ShortID{
								ids.GenerateTestShortID(),
							},
						},
					},
				},
			},
			tc.WithDefaultContext(),
		)
		require.NoError(err)
		awaitTxAccepted(tc, acceptanceURI, tx.ID())
	})
	epochAfter, err := proposerClient.GetCurrentEpoch(tc.DefaultContext())
	require.NoError(err)
	require.Greater(epochAfter.PChainHeight, epochBefore.PChainHeight)
}

// l1Manager issues manager-originated validator changes for an L1.
type l1Manager struct {
	pWallet      pwallet.Wallet
	genesisPeer  *peer.Peer
	peerMessages buffer.BlockingDeque[*p2pmessage.InboundMessage]
	networkID    uint32
	chainID      ids.ID
	address      []byte
	// acceptanceURI is the API URI polled to confirm issued txs are accepted.
	acceptanceURI string
}

// registerValidator signs [message] as the manager and issues the RegisterL1ValidatorTx, funding the validator with [balance] and proving ownership of its BLS key with [pop]
func (m *l1Manager) registerValidator(
	tc tests.TestContext,
	message *warpmessage.RegisterL1Validator,
	balance uint64,
	pop [bls.SignatureLen]byte,
) {
	require := require.New(tc)
	tc.By("creating the unsigned warp message")
	unsigned := newAddressedCallMessage(tc, m.networkID, m.chainID, m.address, message.Bytes())

	signature := requestWarpSignature(tc, m.genesisPeer, m.peerMessages, unsigned, nil)

	tc.By("creating the signed warp message to register the validator")
	signed, err := warp.NewMessage(
		unsigned,
		&warp.BitSetSignature{
			Signers: set.NewBits(0).Bytes(), // [signers] has weight from the genesis peer
			Signature: ([bls.SignatureLen]byte)(
				bls.SignatureToBytes(signature),
			),
		},
	)
	require.NoError(err)

	tc.By("issuing a RegisterL1ValidatorTx", func() {
		tx, err := m.pWallet.IssueRegisterL1ValidatorTx(
			balance,
			pop,
			signed.Bytes(),
		)
		require.NoError(err)
		awaitTxAccepted(tc, m.acceptanceURI, tx.ID())
	})
}

func (m *l1Manager) setValidatorWeight(
	tc tests.TestContext,
	validationID ids.ID,
	nonce uint64,
	weight uint64,
) {
	require := require.New(tc)

	tc.By("creating the unsigned L1ValidatorWeightMessage")
	weightMessage, err := warpmessage.NewL1ValidatorWeight(validationID, nonce, weight)
	require.NoError(err)

	unsigned := newAddressedCallMessage(tc, m.networkID, m.chainID, m.address, weightMessage.Bytes())

	signature := requestWarpSignature(tc, m.genesisPeer, m.peerMessages, unsigned, nil)

	tc.By("creating the signed warp message to set the weight of the validator")
	signed, err := warp.NewMessage(
		unsigned,
		&warp.BitSetSignature{
			Signers: set.NewBits(0).Bytes(), // [signers] has weight from the genesis peer
			Signature: ([bls.SignatureLen]byte)(
				bls.SignatureToBytes(signature),
			),
		},
	)
	require.NoError(err)

	tc.By("issuing a SetL1ValidatorWeightTx", func() {
		tx, err := m.pWallet.IssueSetL1ValidatorWeightTx(signed.Bytes())
		require.NoError(err)
		awaitTxAccepted(tc, m.acceptanceURI, tx.ID())
	})
}

func wrapWarpSignatureRequest(
	msg *warp.UnsignedMessage,
	justification []byte,
) (*p2pmessage.OutboundMessage, error) {
	p2pMessageFactory, err := p2pmessage.NewCreator(
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		p2pTimeout,
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
		msg.SourceChainID,
		0,
		time.Hour,
		p2psdk.PrefixMessage(
			p2psdk.ProtocolPrefix(p2psdk.SignatureRequestHandlerID),
			requestBytes,
		),
	)
}

func findMessage[T any](
	q buffer.BlockingDeque[*p2pmessage.InboundMessage],
	parser func(*p2pmessage.InboundMessage) (T, bool, error),
) (T, bool, error) {
	var messagesToReprocess []*p2pmessage.InboundMessage
	defer func() {
		slices.Reverse(messagesToReprocess)
		for _, msg := range messagesToReprocess {
			q.PushLeft(msg)
		}
	}()

	for {
		msg, ok := q.PopLeft()
		if !ok {
			return utils.Zero[T](), false, nil
		}

		parsed, ok, err := parser(msg)
		if err != nil {
			return utils.Zero[T](), false, err
		}
		if ok {
			return parsed, true, nil
		}

		messagesToReprocess = append(messagesToReprocess, msg)
	}
}

// unwrapWarpSignature assumes the only type of AppResponses that will be
// received are ACP-118 compliant responses.
func unwrapWarpSignature(msg *p2pmessage.InboundMessage) (*bls.Signature, bool, error) {
	var appResponse *p2ppb.AppResponse
	switch msg := msg.Message.(type) {
	case *p2ppb.AppResponse:
		appResponse = msg
	case *p2ppb.AppError:
		return nil, false, errors.New(msg.ErrorMessage)
	default:
		return nil, false, nil
	}

	var response sdk.SignatureResponse
	if err := proto.Unmarshal(appResponse.AppBytes, &response); err != nil {
		return nil, false, err
	}

	warpSignature, err := bls.SignatureFromBytes(response.Signature)
	return warpSignature, true, err
}
