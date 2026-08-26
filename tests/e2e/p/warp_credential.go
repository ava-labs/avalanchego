// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"
	"encoding/hex"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	warpclient "github.com/ava-labs/avalanchego/graft/coreth/warp"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/warpauth"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// An EVM address creates a subnet on the P-chain with one ordinary C-chain
// transaction: the helper contract builds the P-chain tx and sends it over
// warp, a keyless relayer wraps the signed message as the credential.
var _ = e2e.DescribePChain("[Warp Credential]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("creates a subnet from a C-chain call", func() {
		env := e2e.GetEnv(tc)

		// The P-chain must trust the helper before it is deployed, so pin the
		// address the pre-funded key will deploy to with its first tx.
		// ponytail: the real thing pins a Nick-deployed address instead.
		privateNetwork := tmpnet.NewDefaultNetwork("avalanchego-e2e-warp-credential")
		privateNetwork.DefaultFlags = tmpnet.FlagsMap{}
		privateNetwork.DefaultFlags.SetDefaults(env.GetNetwork().DefaultFlags)
		keys, err := tmpnet.NewPrivateKeys(1)
		require.NoError(err)
		privateNetwork.PreFundedKeys = keys
		key := keys[0]
		ethAddress := key.EthAddress()
		owner := ids.ShortID(ethAddress)
		helper := crypto.CreateAddress(ethAddress, 0)
		privateNetwork.PrimaryChainConfigs = map[string]tmpnet.ConfigMap{
			"P": {"warp-helper-addresses": []string{ids.ShortID(helper).String()}},
		}
		env.StartPrivateNetwork(privateNetwork)
		e2e.EmitMetricsLink = false

		node := privateNetwork.Nodes[0]
		nodeURI := tmpnet.NodeURI{NodeID: node.NodeID, URI: node.GetAccessibleURI()}
		keychain := secp256k1fx.NewKeychain(key)
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		pWallet := baseWallet.P()
		pContext := pWallet.Builder().Context()
		pClient := platformvm.NewClient(nodeURI.URI)
		ethClient := e2e.NewEthClient(tc, nodeURI)

		networkID, err := info.NewClient(nodeURI.URI).GetNetworkID(tc.DefaultContext())
		require.NoError(err)

		const fundAmount = 100 * units.MilliAvax
		var utxoID avax.UTXOID
		tc.By("funding the EVM address on the P-chain", func() {
			fundTx, err := pWallet.IssueBaseTx(
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: pContext.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          fundAmount,
						OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{owner}},
					},
				}},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
			for i, out := range fundTx.Unsigned.Outputs() {
				if out.Out.(*secp256k1fx.TransferOutput).Addrs[0] == owner {
					utxoID = avax.UTXOID{TxID: fundTx.ID(), OutputIndex: uint32(i)}
				}
			}
			require.NotEqual(ids.Empty, utxoID.TxID)
		})

		cChainID, err := ethClient.ChainID(tc.DefaultContext())
		require.NoError(err)
		signer := types.NewLondonSigner(cChainID)
		nonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), ethAddress)
		require.NoError(err)
		require.Zero(nonce)
		gasPrice := e2e.SuggestGasPrice(tc, ethClient)
		send := func(to *common.Address, data []byte) *types.Receipt {
			tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
				ChainID:   cChainID,
				Nonce:     nonce,
				GasTipCap: big.NewInt(0),
				GasFeeCap: gasPrice,
				Gas:       8_000_000,
				To:        to,
				Data:      data,
			}), signer, key.ToECDSA())
			require.NoError(err)
			nonce++
			receipt := e2e.SendEthTransaction(tc, ethClient, tx)
			require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
			return receipt
		}

		parsedABI, err := abi.JSON(strings.NewReader(warpauth.PChainABI))
		require.NoError(err)

		tc.By("deploying the helper contract on the C-chain", func() {
			initcode, err := hex.DecodeString(warpauth.PChainBin)
			require.NoError(err)
			ctorArgs, err := parsedABI.Pack("", networkID, constants.PlatformChainID, pContext.AVAXAssetID)
			require.NoError(err)
			require.Equal(helper, send(nil, append(initcode, ctorArgs...)).ContractAddress)
		})

		tc.By("starting a keyless relayer", func() {
			// WARPAUTH_RELAYER_CMD runs an external relayer (the icm-services
			// pchain-relayer) instead of the in-process one; the shell gets
			// NODE_URI and HELPER. Pair with --activate-latest-after 0 to
			// run it against SAE, which has no aggregation RPC.
			if cmd := os.Getenv("WARPAUTH_RELAYER_CMD"); cmd != "" {
				relayer := exec.Command("sh", "-c", "exec "+cmd)
				relayer.Env = append(os.Environ(), "NODE_URI="+nodeURI.URI, "HELPER="+helper.Hex())
				relayer.Stdout = os.Stdout
				relayer.Stderr = os.Stderr
				require.NoError(relayer.Start())
				tc.DeferCleanup(func() {
					_ = relayer.Process.Kill()
					_ = relayer.Wait()
				})
				return
			}
			warpClient, err := warpclient.NewClient(nodeURI.URI, "C")
			require.NoError(err)
			relayer := &warpauth.Relayer{
				Log: tc.Log(),
				Eth: ethClient,
				Sign: func(ctx context.Context, msg *avalanchewarp.UnsignedMessage) ([]byte, error) {
					return warpClient.GetMessageAggregateSignature(ctx, msg.ID(), 67, "")
				},
				PChain: pClient,
				Helper: helper,
			}
			ctx, cancel := context.WithCancel(tc.DefaultContext())
			tc.DeferCleanup(cancel)
			go func() { _ = relayer.Run(ctx, 0) }()
		})

		// command sends one helper call from the EVM address; the relayer
		// does the rest.
		command := func(method string, args ...any) {
			data, err := parsedABI.Pack(method, args...)
			require.NoError(err)
			send(&helper, data)
		}
		subnetOwnedBy := func(subnetID ids.ID, addr ids.ShortID) bool {
			subnet, err := pClient.GetSubnet(tc.DefaultContext(), subnetID)
			return err == nil && len(subnet.ControlKeys) == 1 && subnet.ControlKeys[0] == addr
		}

		type utxo struct {
			TxID        [32]byte
			OutputIndex uint32
			Amount      uint64
		}
		type owners struct {
			Locktime  uint64
			Threshold uint32
			Addrs     []common.Address
		}
		const fee = 10 * units.MilliAvax

		var subnetID ids.ID
		tc.By("creating a subnet owned by the EVM address", func() {
			command("createSubnet",
				[]utxo{{TxID: utxoID.TxID, OutputIndex: utxoID.OutputIndex, Amount: fundAmount}},
				uint64(fundAmount-fee),
				owners{Threshold: 1, Addrs: []common.Address{ethAddress}},
			)
			tc.Eventually(func() bool {
				subnets, err := pClient.GetSubnets(tc.DefaultContext(), nil)
				require.NoError(err)
				for _, subnet := range subnets {
					if len(subnet.ControlKeys) == 1 && subnet.ControlKeys[0] == owner {
						subnetID = subnet.ID
						return true
					}
				}
				return false
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "subnet not created")
			// The change output is the only output.
			utxoID = avax.UTXOID{TxID: subnetID, OutputIndex: 0}
		})

		tc.By("transferring the subnet with the EVM address as subnet authority", func() {
			newOwner := ids.GenerateTestShortID()
			command("transferSubnetOwnership",
				[]utxo{{TxID: utxoID.TxID, OutputIndex: utxoID.OutputIndex, Amount: fundAmount - fee}},
				uint64(fundAmount-2*fee),
				subnetID,
				[]uint32{0},
				owners{Threshold: 1, Addrs: []common.Address{common.Address(newOwner)}},
			)
			tc.Eventually(func() bool {
				return subnetOwnedBy(subnetID, newOwner)
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "subnet not transferred")
		})

		// balance sums the owner's spendable AVAX on the P-chain.
		balance := func() uint64 {
			utxosBytes, _, _, err := pClient.GetUTXOs(tc.DefaultContext(), []ids.ShortID{owner}, 1024, ids.ShortEmpty, ids.Empty)
			require.NoError(err)
			var total uint64
			for _, utxoBytes := range utxosBytes {
				utxo := &avax.UTXO{}
				_, err := txs.Codec.Unmarshal(utxoBytes, utxo)
				require.NoError(err)
				total += utxo.Out.(avax.Amounter).Amount()
			}
			return total
		}

		const stakeAmount = 25 * units.Avax
		var stakeUTXO avax.UTXOID
		tc.By("funding the EVM address for staking", func() {
			fundTx, err := pWallet.IssueBaseTx(
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: pContext.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          stakeAmount + fee,
						OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{owner}},
					},
				}},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
			for i, out := range fundTx.Unsigned.Outputs() {
				if out.Out.(*secp256k1fx.TransferOutput).Addrs[0] == owner {
					stakeUTXO = avax.UTXOID{TxID: fundTx.ID(), OutputIndex: uint32(i)}
				}
			}
		})

		type validator struct {
			NodeID [20]byte
			Start  uint64
			End    uint64
			Weight uint64
		}
		type out struct {
			Amount uint64
			Owners owners
		}
		validatorNodeID := node.NodeID
		endTime := uint64(time.Now().Add(20 * time.Second).Unix())
		before := balance()
		tc.By("delegating to a primary network validator from the EVM address", func() {
			command("addPermissionlessDelegator",
				[]utxo{{TxID: stakeUTXO.TxID, OutputIndex: stakeUTXO.OutputIndex, Amount: stakeAmount + fee}},
				uint64(0),
				validator{NodeID: validatorNodeID, End: endTime, Weight: stakeAmount},
				constants.PrimaryNetworkID,
				[]out{{Amount: stakeAmount, Owners: owners{Threshold: 1, Addrs: []common.Address{ethAddress}}}},
				owners{Threshold: 1, Addrs: []common.Address{ethAddress}},
			)
			tc.Eventually(func() bool {
				vdrs, err := pClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{validatorNodeID})
				require.NoError(err)
				for _, vdr := range vdrs {
					for _, d := range vdr.Delegators {
						if d.RewardOwner != nil && len(d.RewardOwner.Addresses) == 1 && d.RewardOwner.Addresses[0] == owner {
							return true
						}
					}
				}
				return false
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "delegation not active")
			require.Equal(before-stakeAmount-fee, balance())
		})

		tc.By("receiving the stake and the reward back on the EVM address", func() {
			tc.Eventually(func() bool {
				return balance() > before-fee
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "stake and reward not returned")
		})
	})
})
