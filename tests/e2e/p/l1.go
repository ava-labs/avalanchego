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
		genesisPeer, genesisPeerMessages := startTestPeer(tc, subnetGenesisNode, networkID)

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
				subnetToL1Conversion, err := warpmessage.NewSubnetToL1Conversion(expectedConversionID)
				require.NoError(err)

				verifyAttestation(tc, genesisPeer, genesisPeerMessages, networkID, genesisNodePK, subnetToL1Conversion.Bytes(), subnetID[:])
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

		manager := &l1ValidatorManager{
			tc:        tc,
			pWallet:   pWallet,
			networkID: networkID,
			chainID:   chainID,
			address:   address,
			peer:      genesisPeer,
			messages:  genesisPeerMessages,
			uri:       subnetGenesisNodeURI,
		}

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
			manager.registerValidator(registerL1ValidatorMessage, registerBalance, registerNodePoP.ProofOfPossession)
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
				l1ValidatorRegistration, err := warpmessage.NewL1ValidatorRegistration(
					registerValidationID,
					true, // registered
				)
				require.NoError(err)

				verifyAttestation(tc, genesisPeer, genesisPeerMessages, networkID, genesisNodePK, l1ValidatorRegistration.Bytes(), nil)
			})
		})

		tc.By("increasing the weight of the validator", func() {
			manager.setWeight(registerValidationID, updatedWeight)
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
						MinNonce: manager.nonce,
						Balance:  0,
					},
					l1Validator,
				)
			})

			tc.By("fetching the validator weight change attestation", func() {
				l1ValidatorWeight, err := warpmessage.NewL1ValidatorWeight(
					registerValidationID,
					manager.nonce-1, // Use the prior nonce
					updatedWeight,
				)
				require.NoError(err)

				verifyAttestation(tc, genesisPeer, genesisPeerMessages, networkID, genesisNodePK, l1ValidatorWeight.Bytes(), nil)
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
			manager.setWeight(registerValidationID, 0)
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
				l1ValidatorRegistration, err := warpmessage.NewL1ValidatorRegistration(
					registerValidationID,
					false, // removed
				)
				require.NoError(err)

				justification := platformvmpb.L1ValidatorRegistrationJustification{
					Preimage: &platformvmpb.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{
						RegisterL1ValidatorMessage: registerL1ValidatorMessage.Bytes(),
					},
				}
				justificationBytes, err := proto.Marshal(&justification)
				require.NoError(err)

				verifyAttestation(tc, genesisPeer, genesisPeerMessages, networkID, genesisNodePK, l1ValidatorRegistration.Bytes(), justificationBytes)
			})
		})

		closeTestPeer(tc, genesisPeer, genesisPeerMessages)

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})

	ginkgo.It("atomically creates an L1 using CreateL1Tx", func() {
		env := e2e.GetEnv(tc)
		nodeURI := env.GetRandomNodeURI()

		tc.By("loading the wallet")
		var (
			keychain   = env.NewKeychain()
			baseWallet = e2e.NewWallet(tc, keychain, nodeURI)
			pWallet    = baseWallet.P()
			pClient    = platformvm.NewClient(nodeURI.URI)
		)

		tc.By("creating the chain genesis")
		genesisBytes := newXSVMGenesisBytes(tc)

		// Start the genesis validator node without subnet tracking since
		// the subnetID (= CreateL1Tx txID) is not known until after issuance.
		tc.By("creating the genesis validator")
		subnetGenesisNode := e2e.AddEphemeralNode(tc, env.GetNetwork(), tmpnet.NewEphemeralNode(tmpnet.FlagsMap{}))

		genesisNodePoP, genesisNodePK := nodeProofOfPossession(tc, subnetGenesisNode)

		// The manager lives on the chain created by this tx: the sentinel
		// resolves to the txID (== subnetID == chainID) at execution.
		address := []byte{}

		var createL1Tx *txs.Tx
		tc.By("issuing a CreateL1Tx", func() {
			tx, err := pWallet.IssueCreateL1Tx(
				constants.XSVMID,
				genesisBytes,
				txs.SelfManagerChainID,
				address,
				[]*txs.CreateL1Validator{
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
			createL1Tx = tx
		})

		// subnetID == chainID == txID for CreateL1Tx. The sentinel manager
		// chainID resolved to the same value.
		subnetID := createL1Tx.ID()
		genesisValidationID := subnetID.Append(0)

		expectedConversionID, err := warpmessage.SubnetToL1ConversionID(warpmessage.SubnetToL1ConversionData{
			SubnetID:       subnetID,
			ManagerChainID: subnetID,
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

		tc.By("verifying the L1 was created", func() {
			tc.By("verifying the subnet reports as an L1", func() {
				subnet, err := pClient.GetSubnet(tc.DefaultContext(), subnetID)
				require.NoError(err)
				require.False(subnet.IsPermissioned)
				require.Equal(expectedConversionID, subnet.ConversionID)
				require.Equal(subnetID, subnet.ManagerChainID)
				require.Equal(address, subnet.ManagerAddress)
			})

			tc.By("verifying the validator set was initialized", func() {
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
		})

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})

	ginkgo.It("creates an L1 using CreateL1Tx and updates its validators via the manager", func() {
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
		)

		tc.By("creating the chain genesis")
		genesisBytes := newXSVMGenesisBytes(tc)

		// The subnetID (= CreateL1Tx txID) is not known until after issuance,
		// so the genesis validator starts without tracking and is restarted
		// with tracking after the L1 is created.
		tc.By("creating the genesis validator")
		subnetGenesisNode := e2e.AddEphemeralNode(tc, env.GetNetwork(), tmpnet.NewEphemeralNode(tmpnet.FlagsMap{}))

		genesisNodePoP, genesisNodePK := nodeProofOfPossession(tc, subnetGenesisNode)

		// The manager lives on the chain created by this tx: the sentinel
		// resolves to the txID (== subnetID == chainID) at execution.
		address := []byte{}

		var createL1Tx *txs.Tx
		tc.By("issuing a CreateL1Tx", func() {
			tx, err := pWallet.IssueCreateL1Tx(
				constants.XSVMID,
				genesisBytes,
				txs.SelfManagerChainID,
				address,
				[]*txs.CreateL1Validator{
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
			createL1Tx = tx
		})

		// subnetID == chainID == txID for CreateL1Tx. The sentinel manager
		// chainID resolved to the same value.
		subnetID := createL1Tx.ID()
		managerChainID := subnetID

		tc.By("verifying the L1 was created", func() {
			expectedConversionID, err := warpmessage.SubnetToL1ConversionID(warpmessage.SubnetToL1ConversionData{
				SubnetID:       subnetID,
				ManagerChainID: managerChainID,
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

			subnet, err := pClient.GetSubnet(tc.DefaultContext(), subnetID)
			require.NoError(err)
			require.False(subnet.IsPermissioned)
			require.Equal(expectedConversionID, subnet.ConversionID)
			require.Equal(managerChainID, subnet.ManagerChainID)
			require.Equal(address, subnet.ManagerAddress)
		})

		tc.By("restarting the genesis validator with the L1 tracked", func() {
			subnetGenesisNode.Flags[config.TrackSubnetsKey] = subnetID.String()
			require.NoError(subnetGenesisNode.Restart(tc.DefaultContext()))
			e2e.WaitForHealthy(tc, subnetGenesisNode)
		})

		// Re-derive the URI after the restart in case ports changed.
		subnetGenesisNodeURI := subnetGenesisNode.GetAccessibleURI()

		tc.By("connecting to the genesis validator")
		networkID := env.GetNetwork().GetNetworkID()
		genesisPeer, genesisPeerMessages := startTestPeer(tc, subnetGenesisNode, networkID)

		tc.By("verifying the validator set was initialized", func() {
			verifyValidatorSet(tc, pClient, subnetID, map[ids.NodeID]*snowvalidators.GetValidatorOutput{
				subnetGenesisNode.NodeID: {
					NodeID:    subnetGenesisNode.NodeID,
					PublicKey: genesisNodePK,
					Weight:    genesisWeight,
				},
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

		manager := &l1ValidatorManager{
			tc:        tc,
			pWallet:   pWallet,
			networkID: networkID,
			chainID:   managerChainID,
			address:   address,
			peer:      genesisPeer,
			messages:  genesisPeerMessages,
			uri:       subnetGenesisNodeURI,
		}

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
			manager.registerValidator(registerL1ValidatorMessage, registerBalance, registerNodePoP.ProofOfPossession)
		})

		tc.By("verifying the validator was registered", func() {
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

		tc.By("increasing the weight of the validator", func() {
			manager.setWeight(registerValidationID, updatedWeight)
		})

		tc.By("issuing an IncreaseL1ValidatorBalanceTx", func() {
			_, err := pWallet.IssueIncreaseL1ValidatorBalanceTx(
				registerValidationID,
				units.Avax,
			)
			require.NoError(err)
		})

		tc.By("verifying the validator was activated with the increased weight", func() {
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

		tc.By("advancing the proposervm P-chain height", func() {
			advanceProposerVMPChainHeight(tc, pWallet, infoClient, proposerClient, subnetGenesisNodeURI)
		})

		tc.By("removing the registered validator", func() {
			manager.setWeight(registerValidationID, 0)
		})

		tc.By("verifying the validator was removed", func() {
			verifyValidatorSet(tc, pClient, subnetID, map[ids.NodeID]*snowvalidators.GetValidatorOutput{
				subnetGenesisNode.NodeID: {
					NodeID:    subnetGenesisNode.NodeID,
					PublicKey: genesisNodePK,
					Weight:    genesisWeight,
				},
			})
		})

		closeTestPeer(tc, genesisPeer, genesisPeerMessages)

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})

// l1ValidatorManager updates an L1's validator set on behalf of the L1's
// validator manager by crafting the manager's warp messages, collecting a
// signature over them from a connected validator, and issuing the resulting
// P-chain transactions.
type l1ValidatorManager struct {
	tc        tests.TestContext
	pWallet   pwallet.Wallet
	networkID uint32
	chainID   ids.ID // chain the validator manager lives on
	address   []byte // address of the validator manager
	peer      *peer.Peer
	messages  buffer.BlockingDeque[*p2pmessage.InboundMessage]
	uri       string // URI of the node to await tx acceptance at
	nonce     uint64
}

// sign collects the connected peer's signature over [unsignedMessage] and
// wraps it into a signed warp message.
func (m *l1ValidatorManager) sign(unsignedMessage *warp.UnsignedMessage) *warp.Message {
	signature := requestWarpSignature(m.tc, m.peer, m.messages, unsignedMessage, nil)

	m.tc.By("creating the signed warp message")
	message, err := warp.NewMessage(
		unsignedMessage,
		&warp.BitSetSignature{
			Signers: set.NewBits(0).Bytes(), // [Signers] has weight from the connected peer
			Signature: ([bls.SignatureLen]byte)(
				bls.SignatureToBytes(signature),
			),
		},
	)
	require.NoError(m.tc, err)
	return message
}

// registerValidator issues a RegisterL1ValidatorTx authorized by [msg] and
// waits for its acceptance.
func (m *l1ValidatorManager) registerValidator(
	msg *warpmessage.RegisterL1Validator,
	balance uint64,
	proofOfPossession [bls.SignatureLen]byte,
) {
	tc := m.tc
	require := require.New(tc)

	tc.By("creating the unsigned RegisterL1ValidatorMessage")
	unsignedRegisterL1Validator := newAddressedCallMessage(tc, m.networkID, m.chainID, m.address, msg.Bytes())

	registerL1Validator := m.sign(unsignedRegisterL1Validator)

	tc.By("issuing a RegisterL1ValidatorTx", func() {
		tx, err := m.pWallet.IssueRegisterL1ValidatorTx(
			balance,
			proofOfPossession,
			registerL1Validator.Bytes(),
		)
		require.NoError(err)

		awaitTxAccepted(tc, m.uri, tx.ID())
	})
}

// setWeight issues a SetL1ValidatorWeightTx setting the weight of
// [validationID] to [weight] and waits for its acceptance. A weight of 0
// removes the validator.
func (m *l1ValidatorManager) setWeight(validationID ids.ID, weight uint64) {
	tc := m.tc
	require := require.New(tc)

	tc.By("creating the unsigned L1ValidatorWeightMessage")
	l1ValidatorWeight, err := warpmessage.NewL1ValidatorWeight(
		validationID,
		m.nonce,
		weight,
	)
	require.NoError(err)
	unsignedL1ValidatorWeight := newAddressedCallMessage(tc, m.networkID, m.chainID, m.address, l1ValidatorWeight.Bytes())

	setL1ValidatorWeight := m.sign(unsignedL1ValidatorWeight)

	tc.By("issuing a SetL1ValidatorWeightTx", func() {
		tx, err := m.pWallet.IssueSetL1ValidatorWeightTx(
			setL1ValidatorWeight.Bytes(),
		)
		require.NoError(err)

		awaitTxAccepted(tc, m.uri, tx.ID())
	})

	m.nonce++
}

// newXSVMGenesisBytes returns the genesis bytes for an XSVM chain with a
// single, newly-generated allocation.
func newXSVMGenesisBytes(t require.TestingT) []byte {
	genesisKey, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	genesisBytes, err := genesis.Codec.Marshal(genesis.CodecVersion, &genesis.Genesis{
		Timestamp: time.Now().Unix(),
		Allocations: []genesis.Allocation{
			{
				Address: genesisKey.Address(),
				Balance: math.MaxUint64,
			},
		},
	})
	require.NoError(t, err)
	return genesisBytes
}

// nodeProofOfPossession returns the BLS proof of possession for [node] along
// with its parsed public key.
func nodeProofOfPossession(t require.TestingT, node *tmpnet.Node) (*signer.ProofOfPossession, *bls.PublicKey) {
	pop, err := node.GetProofOfPossession()
	require.NoError(t, err)

	pk, err := bls.PublicKeyFromCompressedBytes(pop.PublicKey[:])
	require.NoError(t, err)

	return pop, pk
}

// startTestPeer connects a test peer to [node] and returns the peer along
// with a deque of the messages it receives.
func startTestPeer(
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

// closeTestPeer closes [p] and its message deque.
func closeTestPeer(
	tc tests.TestContext,
	p *peer.Peer,
	messages buffer.BlockingDeque[*p2pmessage.InboundMessage],
) {
	messages.Close()
	p.StartClose()
	require.NoError(tc, p.AwaitClosed(tc.DefaultContext()))
}

// awaitTxAccepted polls the node at [uri] until it reports [txID] as
// accepted.
func awaitTxAccepted(tc tests.TestContext, uri string, txID ids.ID) {
	tc.By("ensuring the tx has been accepted at "+uri, func() {
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

// verifyValidatorSet checks that the validator set of [subnetID] at the
// current height matches [expectedValidators].
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

	// Test GetAllValidatorsAt too, for coverage
	flattenedExpectedValidators, err := snowvalidators.FlattenValidatorSet(expectedValidators)
	require.NoError(err)

	// require.Equal will complain if one has a nil slice and the other
	// has an empty slice. This avoids that issue.
	if len(flattenedExpectedValidators.Validators) == 0 {
		flattenedExpectedValidators.Validators = nil
	}

	allValidators, err := pClient.GetAllValidatorsAt(tc.DefaultContext(), platformapi.Height(height))
	require.NoError(err)
	require.Equal(flattenedExpectedValidators, allValidators[subnetID])
}

// advanceProposerVMPChainHeight ensures that the P-chain height that new
// proposervm blocks reference includes the most recently accepted blocks,
// either by waiting out the recently accepted window or, post-Granite, by
// advancing the epoch.
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
		// Wait to ensure the next block will reference the last
		// accepted P-chain height.
		time.Sleep(timeToAdvancePChainWindow)
		return
	}

	epochBefore, err := proposerClient.GetCurrentEpoch(tc.DefaultContext())
	require.NoError(err)

	tc.By("waiting", func() {
		timeToAdvanceEpoch := max(timeToAdvancePChainWindow, upgrades.GraniteEpochDuration)
		time.Sleep(timeToAdvanceEpoch)
	})

	tc.By("issuing a dummy tx to advance the epoch", func() {
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

// newAddressedCallMessage returns an unsigned warp message from
// [sourceChainID] wrapping an AddressedCall of [msg] from [sourceAddress].
func newAddressedCallMessage(
	t require.TestingT,
	networkID uint32,
	sourceChainID ids.ID,
	sourceAddress []byte,
	msg []byte,
) *warp.UnsignedMessage {
	addressedCall, err := payload.NewAddressedCall(sourceAddress, msg)
	require.NoError(t, err)

	unsignedMessage, err := warp.NewUnsignedMessage(networkID, sourceChainID, addressedCall.Bytes())
	require.NoError(t, err)

	return unsignedMessage
}

// requestWarpSignature requests the connected peer's signature over
// [unsignedMessage], optionally justified by [justification], and returns it.
func requestWarpSignature(
	tc tests.TestContext,
	p *peer.Peer,
	messages buffer.BlockingDeque[*p2pmessage.InboundMessage],
	unsignedMessage *warp.UnsignedMessage,
	justification []byte,
) *bls.Signature {
	require := require.New(tc)

	tc.By("sending the request to sign the warp message", func() {
		request, err := wrapWarpSignatureRequest(unsignedMessage, justification)
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

// verifyAttestation requests the connected peer's signature over a P-chain
// attestation of [msg], optionally justified by [justification], and verifies
// the signature against [pk].
func verifyAttestation(
	tc tests.TestContext,
	p *peer.Peer,
	messages buffer.BlockingDeque[*p2pmessage.InboundMessage],
	networkID uint32,
	pk *bls.PublicKey,
	msg []byte,
	justification []byte,
) {
	unsignedMessage := newAddressedCallMessage(tc, networkID, constants.PlatformChainID, nil, msg)
	signature := requestWarpSignature(tc, p, messages, unsignedMessage, justification)
	require.True(tc, bls.Verify(pk, signature, unsignedMessage.Bytes()))
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
