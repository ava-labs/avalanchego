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
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	pwarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
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
			"P": {"warp-helper-address": ids.ShortID(helper).String()},
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
				Gas:       2_000_000,
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

		var unsignedMsg *pwarp.UnsignedMessage
		tc.By("calling createSubnet from the EVM address", func() {
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
			data, err := parsedABI.Pack("createSubnet",
				[]utxo{{TxID: utxoID.TxID, OutputIndex: utxoID.OutputIndex, Amount: fundAmount}},
				uint64(fundAmount-10*units.MilliAvax),
				owners{Threshold: 1, Addrs: []common.Address{ethAddress}},
			)
			require.NoError(err)
			receipt := send(&helper, data)
			for _, log := range receipt.Logs {
				if log.Address != warp.ContractAddress {
					continue
				}
				unsignedMsg, err = warp.UnpackSendWarpEventDataToMessage(log.Data)
				require.NoError(err)
			}
			require.NotNil(unsignedMsg)
		})

		var signedMsg []byte
		tc.By("aggregating validator signatures", func() {
			client, err := warpclient.NewClient(nodeURI.URI, "C")
			require.NoError(err)
			tc.Eventually(func() bool {
				signedMsg, err = client.GetMessageAggregateSignature(tc.DefaultContext(), unsignedMsg.ID(), 67, "")
				return err == nil
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to aggregate signatures")
		})

		var pTx *txs.Tx
		tc.By("relaying the message to the P-chain", func() {
			call, err := payload.ParseAddressedCall(unsignedMsg.Payload)
			require.NoError(err)
			require.Equal(helper.Bytes(), call.SourceAddress)
			require.Equal(owner[:], call.Payload[:ids.ShortIDLen])

			var unsigned txs.UnsignedTx
			_, err = txs.Codec.Unmarshal(call.Payload[ids.ShortIDLen:], &unsigned)
			require.NoError(err)
			pTx = &txs.Tx{
				Unsigned: unsigned,
				Creds:    []verify.Verifiable{&secp256k1fx.WarpCredential{Message: signedMsg}},
			}
			require.NoError(pTx.Initialize(txs.Codec))

			txID, err := pClient.IssueTx(tc.DefaultContext(), pTx.Bytes())
			require.NoError(err)
			require.Equal(pTx.ID(), txID)
			tc.Eventually(func() bool {
				res, err := pClient.GetTxStatus(tc.DefaultContext(), txID)
				require.NoError(err)
				return res.Status == status.Committed
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "tx not committed")
		})

		tc.By("checking the subnet is owned by the EVM address", func() {
			subnet, err := pClient.GetSubnet(tc.DefaultContext(), pTx.ID())
			require.NoError(err)
			require.Equal([]ids.ShortID{owner}, subnet.ControlKeys)
			require.Equal(uint32(1), subnet.Threshold)
		})
	})
})
