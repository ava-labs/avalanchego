// Demo bootstrap for the C-chain-with-any-EVM-wallet prototype: starts a
// persistent tmpnet network with SAE from genesis, deploys CChainHelper.sol
// from the ewoq key's nonce-0 tx (so its address is known before the network
// starts and can be trusted in the C-chain config) and writes everything the
// demo page needs to os.Args[2]. The network keeps running after this exits.
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/cchainhelper"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "FATAL:", err)
		os.Exit(1)
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	log := tests.NewDefaultLogger("evmwallet-demo")

	avagoPath := os.Args[1]

	upgrades := upgradetest.GetConfig(upgradetest.Latest)
	upgrades.GraniteEpochDuration = 4 * time.Second
	upgradeJSON, err := json.Marshal(upgrades)
	check(err)

	key := genesis.EWOQKey
	ethAddress := key.EthAddress()
	helper := crypto.CreateAddress(ethAddress, 0)

	network := tmpnet.NewDefaultNetwork("evmwallet-demo")
	// A random chain ID under MetaMask's MAX_SAFE_CHAIN_ID that collides with
	// nothing on chainlist. tmpnet uses the network ID as the eth chain ID, and
	// a non-zero NetworkID field means "join a public network", so it goes
	// through the genesis.
	testGenesis, err := tmpnet.NewTestGenesis(3140518821, network.Nodes, []*secp256k1.PrivateKey{key})
	check(err)
	network.Genesis = testGenesis
	network.DefaultFlags = tmpnet.FlagsMap{
		config.UpgradeFileContentKey: base64.StdEncoding.EncodeToString(upgradeJSON),
	}
	network.DefaultFlags.SetDefaults(tmpnet.DefaultE2EFlags())
	network.PreFundedKeys = []*secp256k1.PrivateKey{key}
	network.PrimaryChainConfigs = map[string]tmpnet.ConfigMap{
		"C": {"helper-addresses": []string{helper.Hex()}},
	}
	network.DefaultRuntimeConfig = tmpnet.NodeRuntimeConfig{
		Process: &tmpnet.ProcessRuntimeConfig{AvalancheGoPath: avagoPath},
	}

	check(tmpnet.BootstrapNewNetwork(ctx, log, network, ""))

	node := network.Nodes[0]
	uri := node.GetAccessibleURI()
	fmt.Println("node up:", uri)

	infoClient := info.NewClient(uri)
	networkID, err := infoClient.GetNetworkID(ctx)
	check(err)
	cChainBlockchainID, err := infoClient.GetBlockchainID(ctx, "C")
	check(err)

	keychain := secp256k1fx.NewKeychain(key)
	wallet, err := primary.MakeWallet(ctx, uri, keychain, keychain, primary.WalletConfig{})
	check(err)
	avaxAssetID := wallet.P().Builder().Context().AVAXAssetID

	// Deploy the helper from ewoq's nonce-0 tx so its address matches the
	// one trusted in the C-chain config.
	ethClient, err := ethclient.Dial(uri + "/ext/bc/C/rpc")
	check(err)
	cChainID, err := ethClient.ChainID(ctx)
	check(err)
	nonce, err := ethClient.NonceAt(ctx, ethAddress, nil)
	check(err)
	if nonce != 0 {
		fmt.Fprintln(os.Stderr, "FATAL: ewoq nonce is not 0, cannot pin helper address")
		os.Exit(1)
	}
	parsedABI, err := abi.JSON(strings.NewReader(cchainhelper.ABI))
	check(err)
	initcode, err := hex.DecodeString(cchainhelper.Bin)
	check(err)
	ctorArgs, err := parsedABI.Pack("", networkID, avaxAssetID)
	check(err)
	gasPrice, err := ethClient.SuggestGasPrice(ctx)
	check(err)
	gasPrice.Mul(gasPrice, big.NewInt(2))
	signer := types.NewLondonSigner(cChainID)
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID:   cChainID,
		Nonce:     0,
		GasTipCap: big.NewInt(0),
		GasFeeCap: gasPrice,
		Gas:       2_000_000,
		Data:      append(initcode, ctorArgs...),
	}), signer, key.ToECDSA())
	check(err)
	check(ethClient.SendTransaction(ctx, tx))
	var receipt *types.Receipt
	for {
		receipt, err = ethClient.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if receipt.Status != types.ReceiptStatusSuccessful || receipt.ContractAddress != helper {
		fmt.Fprintf(os.Stderr, "FATAL: deploy failed status=%d addr=%s want=%s\n", receipt.Status, receipt.ContractAddress, helper)
		os.Exit(1)
	}
	fmt.Println("helper deployed:", helper.Hex())

	hrp := constants.GetHRP(networkID)
	pAddress, err := address.Format("P", hrp, key.Address().Bytes())
	check(err)
	out := map[string]any{
		"uri":                uri,
		"networkID":          networkID,
		"hrp":                hrp,
		"ethChainID":         cChainID.String(),
		"helper":             helper.Hex(),
		"ethAddress":         ethAddress.Hex(),
		"pAddress":           pAddress,
		"cChainBlockchainID": cChainBlockchainID.String(),
		"cChainIDHex":        "0x" + hex.EncodeToString(cChainBlockchainID[:]),
		"avaxAssetID":        avaxAssetID.String(),
		"avaxAssetIDHex":     "0x" + hex.EncodeToString(avaxAssetID[:]),
		"nodeID":             node.NodeID.String(),
		"networkDir":         network.Dir,
	}
	enc, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(enc))
	if len(os.Args) > 2 {
		check(os.WriteFile(os.Args[2], enc, 0o644))
	}
}
