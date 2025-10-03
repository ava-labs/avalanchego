// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	p2pmessage "github.com/ava-labs/avalanchego/message"
	p2psdk "github.com/ava-labs/avalanchego/network/p2p"
	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	platformvmpb "github.com/ava-labs/avalanchego/proto/pb/platformvm"
	snowvalidators "github.com/ava-labs/avalanchego/snow/validators"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	platformvmvalidators "github.com/ava-labs/avalanchego/vms/platformvm/validators"
	warpmessage "github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
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
)

var _ = e2e.DescribePChain("[L1]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("creates and updates L1 validators", func() {
		env := e2e.GetEnv(tc)
		nodeURI := env.GetRandomNodeURI()

		tc.By("loading the wallet")
		var (
			keychain   = env.NewKeychain()
			baseWallet = e2e.NewWallet(tc, keychain, nodeURI)
			pWallet    = baseWallet.P()
			pClient    = platformvm.NewClient(nodeURI.URI)
			owner      = &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
			}
		)

		tc.By("creating the chain genesis")
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

		verifyValidatorSet := func(expectedValidators map[ids.NodeID]*snowvalidators.GetValidatorOutput) {
			height, err := pClient.GetHeight(tc.DefaultContext())
			require.NoError(err)

			subnetValidators, err := pClient.GetValidatorsAt(tc.DefaultContext(), subnetID, platformapi.Height(height))
			require.NoError(err)
			require.Equal(expectedValidators, subnetValidators)

			allValidators, err := pClient.GetAllValidatorsAt(tc.DefaultContext(), platformapi.Height(height))
			require.NoError(err)
			require.Equal(expectedValidators, allValidators[subnetID].Validators)
		}
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
				verifyValidatorSet(map[ids.NodeID]*snowvalidators.GetValidatorOutput{})
			})
		})

		tc.By("creating the genesis validator")
		subnetGenesisNode := e2e.AddEphemeralNode(tc, env.GetNetwork(), tmpnet.NewEphemeralNode(tmpnet.FlagsMap{
			config.TrackSubnetsKey: subnetID.String(),
		}))

		genesisNodePoP, err := subnetGenesisNode.GetProofOfPossession()
		require.NoError(err)

		genesisNodePK, err := bls.PublicKeyFromCompressedBytes(genesisNodePoP.PublicKey[:])
		require.NoError(err)

		tc.By("connecting to the genesis validator")
		var (
			networkID           = env.GetNetwork().GetNetworkID()
			genesisPeerMessages = buffer.NewUnboundedBlockingDeque[p2pmessage.InboundMessage](1)
		)
		stakingAddress, cancel, err := subnetGenesisNode.GetAccessibleStakingAddress(tc.DefaultContext())
		require.NoError(err)
		tc.DeferCleanup(cancel)
		genesisPeer, err := peer.StartTestPeer(
			tc.DefaultContext(),
			stakingAddress,
			networkID,
			router.InboundHandlerFunc(func(_ context.Context, m p2pmessage.InboundMessage) {
				tc.Log().Info("received a message",
					zap.Stringer("op", m.Op()),
					zap.Stringer("message", m.Message()),
					zap.Stringer("from", m.NodeID()),
				)
				genesisPeerMessages.PushRight(m)
			}),
		)
		require.NoError(err)

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

			tc.By("ensuring the genesis peer has accepted the tx at "+subnetGenesisNodeURI, func() {
				var (
					client = platformvm.NewClient(subnetGenesisNodeURI)
					txID   = tx.ID()
				)
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
				verifyValidatorSet(map[ids.NodeID]*snowvalidators.GetValidatorOutput{
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
				unsignedSubnetToL1Conversion := must[*warp.UnsignedMessage](tc)(warp.NewUnsignedMessage(
					networkID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](tc)(payload.NewAddressedCall(
						nil,
						must[*warpmessage.SubnetToL1Conversion](tc)(warpmessage.NewSubnetToL1Conversion(
							expectedConversionID,
						)).Bytes(),
					)).Bytes(),
				))

				tc.By("sending the request to sign the warp message", func() {
					registerL1ValidatorRequest, err := wrapWarpSignatureRequest(
						unsignedSubnetToL1Conversion,
						subnetID[:],
					)
					require.NoError(err)

					require.True(genesisPeer.Send(tc.DefaultContext(), registerL1ValidatorRequest))
				})

				tc.By("getting the signature response", func() {
					signature, ok, err := findMessage(genesisPeerMessages, unwrapWarpSignature)
					require.NoError(err)
					require.True(ok)
					require.True(bls.Verify(genesisNodePK, signature, unsignedSubnetToL1Conversion.Bytes()))
				})
			})
		})

		advanceProposerVMPChainHeight := func() {
			// We must wait at least [RecentlyAcceptedWindowTTL] to ensure the
			// next block will reference the last accepted P-chain height.
			time.Sleep((5 * platformvmvalidators.RecentlyAcceptedWindowTTL) / 4)
		}
		tc.By("advancing the proposervm P-chain height", advanceProposerVMPChainHeight)

		tc.By("creating the validator to register")
		subnetRegisterNode := e2e.AddEphemeralNode(tc, env.GetNetwork(), tmpnet.NewEphemeralNode(tmpnet.FlagsMap{
			config.TrackSubnetsKey: subnetID.String(),
		}))

		registerNodePoP, err := subnetRegisterNode.GetProofOfPossession()
		require.NoError(err)

		registerNodePK, err := bls.PublicKeyFromCompressedBytes(registerNodePoP.PublicKey[:])
		require.NoError(err)

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

		tc.By("registering the validator", func() {
			tc.By("creating the unsigned warp message")
			unsignedRegisterL1Validator := must[*warp.UnsignedMessage](tc)(warp.NewUnsignedMessage(
				networkID,
				chainID,
				must[*payload.AddressedCall](tc)(payload.NewAddressedCall(
					address,
					registerL1ValidatorMessage.Bytes(),
				)).Bytes(),
			))

			tc.By("sending the request to sign the warp message", func() {
				registerL1ValidatorRequest, err := wrapWarpSignatureRequest(
					unsignedRegisterL1Validator,
					nil,
				)
				require.NoError(err)

				require.True(genesisPeer.Send(tc.DefaultContext(), registerL1ValidatorRequest))
			})

			tc.By("getting the signature response")
			registerL1ValidatorSignature, ok, err := findMessage(genesisPeerMessages, unwrapWarpSignature)
			require.NoError(err)
			require.True(ok)

			tc.By("creating the signed warp message to register the validator")
			registerL1Validator, err := warp.NewMessage(
				unsignedRegisterL1Validator,
				&warp.BitSetSignature{
					Signers: set.NewBits(0).Bytes(), // [signers] has weight from the genesis peer
					Signature: ([bls.SignatureLen]byte)(
						bls.SignatureToBytes(registerL1ValidatorSignature),
					),
				},
			)
			require.NoError(err)

			tc.By("issuing a RegisterL1ValidatorTx", func() {
				tx, err := pWallet.IssueRegisterL1ValidatorTx(
					registerBalance,
					registerNodePoP.ProofOfPossession,
					registerL1Validator.Bytes(),
				)
				require.NoError(err)

				tc.By("ensuring the genesis peer has accepted the tx at "+subnetGenesisNodeURI, func() {
					var (
						client = platformvm.NewClient(subnetGenesisNodeURI)
						txID   = tx.ID()
					)
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
			})
		})

		tc.By("verifying the validator was registered", func() {
			tc.By("verifying the validator set was updated", func() {
				verifyValidatorSet(map[ids.NodeID]*snowvalidators.GetValidatorOutput{
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
				unsignedL1ValidatorRegistration := must[*warp.UnsignedMessage](tc)(warp.NewUnsignedMessage(
					networkID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](tc)(payload.NewAddressedCall(
						nil,
						must[*warpmessage.L1ValidatorRegistration](tc)(warpmessage.NewL1ValidatorRegistration(
							registerValidationID,
							true, // registered
						)).Bytes(),
					)).Bytes(),
				))

				tc.By("sending the request to sign the warp message", func() {
					l1ValidatorRegistrationRequest, err := wrapWarpSignatureRequest(
						unsignedL1ValidatorRegistration,
						nil,
					)
					require.NoError(err)

					require.True(genesisPeer.Send(tc.DefaultContext(), l1ValidatorRegistrationRequest))
				})

				tc.By("getting the signature response", func() {
					signature, ok, err := findMessage(genesisPeerMessages, unwrapWarpSignature)
					require.NoError(err)
					require.True(ok)
					require.True(bls.Verify(genesisNodePK, signature, unsignedL1ValidatorRegistration.Bytes()))
				})
			})
		})

		var nextNonce uint64
		setWeight := func(validationID ids.ID, weight uint64) {
			tc.By("creating the unsigned L1ValidatorWeightMessage")
			unsignedL1ValidatorWeight := must[*warp.UnsignedMessage](tc)(warp.NewUnsignedMessage(
				networkID,
				chainID,
				must[*payload.AddressedCall](tc)(payload.NewAddressedCall(
					address,
					must[*warpmessage.L1ValidatorWeight](tc)(warpmessage.NewL1ValidatorWeight(
						validationID,
						nextNonce,
						weight,
					)).Bytes(),
				)).Bytes(),
			))

			tc.By("sending the request to sign the warp message", func() {
				setL1ValidatorWeightRequest, err := wrapWarpSignatureRequest(
					unsignedL1ValidatorWeight,
					nil,
				)
				require.NoError(err)

				require.True(genesisPeer.Send(tc.DefaultContext(), setL1ValidatorWeightRequest))
			})

			tc.By("getting the signature response")
			setL1ValidatorWeightSignature, ok, err := findMessage(genesisPeerMessages, unwrapWarpSignature)
			require.NoError(err)
			require.True(ok)

			tc.By("creating the signed warp message to increase the weight of the validator")
			setL1ValidatorWeight, err := warp.NewMessage(
				unsignedL1ValidatorWeight,
				&warp.BitSetSignature{
					Signers: set.NewBits(0).Bytes(), // [signers] has weight from the genesis validator
					Signature: ([bls.SignatureLen]byte)(
						bls.SignatureToBytes(setL1ValidatorWeightSignature),
					),
				},
			)
			require.NoError(err)

			tc.By("issuing a SetL1ValidatorWeightTx", func() {
				tx, err := pWallet.IssueSetL1ValidatorWeightTx(
					setL1ValidatorWeight.Bytes(),
				)
				require.NoError(err)

				tc.By("ensuring the genesis peer has accepted the tx at "+subnetGenesisNodeURI, func() {
					var (
						client = platformvm.NewClient(subnetGenesisNodeURI)
						txID   = tx.ID()
					)
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
			})

			nextNonce++
		}

		tc.By("increasing the weight of the validator", func() {
			setWeight(registerValidationID, updatedWeight)
		})

		tc.By("verifying the validator weight was increased", func() {
			tc.By("verifying the validator set was updated", func() {
				verifyValidatorSet(map[ids.NodeID]*snowvalidators.GetValidatorOutput{
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
				unsignedL1ValidatorWeight := must[*warp.UnsignedMessage](tc)(warp.NewUnsignedMessage(
					networkID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](tc)(payload.NewAddressedCall(
						nil,
						must[*warpmessage.L1ValidatorWeight](tc)(warpmessage.NewL1ValidatorWeight(
							registerValidationID,
							nextNonce-1, // Use the prior nonce
							updatedWeight,
						)).Bytes(),
					)).Bytes(),
				))

				tc.By("sending the request to sign the warp message", func() {
					l1ValidatorRegistrationRequest, err := wrapWarpSignatureRequest(
						unsignedL1ValidatorWeight,
						nil,
					)
					require.NoError(err)

					require.True(genesisPeer.Send(tc.DefaultContext(), l1ValidatorRegistrationRequest))
				})

				tc.By("getting the signature response", func() {
					signature, ok, err := findMessage(genesisPeerMessages, unwrapWarpSignature)
					require.NoError(err)
					require.True(ok)
					require.True(bls.Verify(genesisNodePK, signature, unsignedL1ValidatorWeight.Bytes()))
				})
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
			verifyValidatorSet(map[ids.NodeID]*snowvalidators.GetValidatorOutput{
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
			verifyValidatorSet(map[ids.NodeID]*snowvalidators.GetValidatorOutput{
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

		tc.By("advancing the proposervm P-chain height", advanceProposerVMPChainHeight)

		tc.By("removing the registered validator", func() {
			setWeight(registerValidationID, 0)
		})

		tc.By("verifying the validator was removed", func() {
			tc.By("verifying the validator set was updated", func() {
				verifyValidatorSet(map[ids.NodeID]*snowvalidators.GetValidatorOutput{
					subnetGenesisNode.NodeID: {
						NodeID:    subnetGenesisNode.NodeID,
						PublicKey: genesisNodePK,
						Weight:    genesisWeight,
					},
				})
			})

			tc.By("fetching the validator removal attestation", func() {
				unsignedL1ValidatorRegistration := must[*warp.UnsignedMessage](tc)(warp.NewUnsignedMessage(
					networkID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](tc)(payload.NewAddressedCall(
						nil,
						must[*warpmessage.L1ValidatorRegistration](tc)(warpmessage.NewL1ValidatorRegistration(
							registerValidationID,
							false, // removed
						)).Bytes(),
					)).Bytes(),
				))

				justification := platformvmpb.L1ValidatorRegistrationJustification{
					Preimage: &platformvmpb.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{
						RegisterL1ValidatorMessage: registerL1ValidatorMessage.Bytes(),
					},
				}
				justificationBytes, err := proto.Marshal(&justification)
				require.NoError(err)

				tc.By("sending the request to sign the warp message", func() {
					l1ValidatorRegistrationRequest, err := wrapWarpSignatureRequest(
						unsignedL1ValidatorRegistration,
						justificationBytes,
					)
					require.NoError(err)

					require.True(genesisPeer.Send(tc.DefaultContext(), l1ValidatorRegistrationRequest))
				})

				tc.By("getting the signature response", func() {
					signature, ok, err := findMessage(genesisPeerMessages, unwrapWarpSignature)
					require.NoError(err)
					require.True(ok)
					require.True(bls.Verify(genesisNodePK, signature, unsignedL1ValidatorRegistration.Bytes()))
				})
			})
		})

		genesisPeerMessages.Close()
		genesisPeer.StartClose()
		require.NoError(genesisPeer.AwaitClosed(tc.DefaultContext()))

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})

func wrapWarpSignatureRequest(
	msg *warp.UnsignedMessage,
	justification []byte,
) (p2pmessage.OutboundMessage, error) {
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
	q buffer.BlockingDeque[p2pmessage.InboundMessage],
	parser func(p2pmessage.InboundMessage) (T, bool, error),
) (T, bool, error) {
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
func unwrapWarpSignature(msg p2pmessage.InboundMessage) (*bls.Signature, bool, error) {
	var appResponse *p2ppb.AppResponse
	switch msg := msg.Message().(type) {
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

func must[T any](t require.TestingT) func(T, error) T {
	return func(val T, err error) T {
		require.NoError(t, err)
		return val
	}
}
