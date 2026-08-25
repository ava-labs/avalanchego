// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"encoding/hex"
	"math/big"
	"strings"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	warpclient "github.com/ava-labs/avalanchego/graft/coreth/warp"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/warpauth"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	pwarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
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

		warpClient, err := warpclient.NewClient(nodeURI.URI, "C")
		require.NoError(err)

		// command sends one helper call from the EVM address, aggregates the
		// validator signatures and relays the wrapped tx to the P-chain.
		command := func(method string, args ...any) *txs.Tx {
			data, err := parsedABI.Pack(method, args...)
			require.NoError(err)
			receipt := send(&helper, data)

			var unsignedMsg *pwarp.UnsignedMessage
			for _, log := range receipt.Logs {
				if log.Address == warp.ContractAddress {
					unsignedMsg, err = warp.UnpackSendWarpEventDataToMessage(log.Data)
					require.NoError(err)
				}
			}
			require.NotNil(unsignedMsg)

			var signedMsg []byte
			tc.Eventually(func() bool {
				signedMsg, err = warpClient.GetMessageAggregateSignature(tc.DefaultContext(), unsignedMsg.ID(), 67, "")
				return err == nil
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to aggregate signatures")

			pTx, err := warpauth.Wrap(signedMsg)
			require.NoError(err)
			txID, err := pClient.IssueTx(tc.DefaultContext(), pTx.Bytes())
			require.NoError(err)
			require.Equal(pTx.ID(), txID)
			tc.Eventually(func() bool {
				res, err := pClient.GetTxStatus(tc.DefaultContext(), txID)
				require.NoError(err)
				return res.Status == status.Committed
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "tx not committed")
			return pTx
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
			createTx := command("createSubnet",
				[]utxo{{TxID: utxoID.TxID, OutputIndex: utxoID.OutputIndex, Amount: fundAmount}},
				uint64(fundAmount-fee),
				owners{Threshold: 1, Addrs: []common.Address{ethAddress}},
			)
			subnetID = createTx.ID()
			subnet, err := pClient.GetSubnet(tc.DefaultContext(), subnetID)
			require.NoError(err)
			require.Equal([]ids.ShortID{owner}, subnet.ControlKeys)
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
			subnet, err := pClient.GetSubnet(tc.DefaultContext(), subnetID)
			require.NoError(err)
			require.Equal([]ids.ShortID{newOwner}, subnet.ControlKeys)
		})
	})
})
