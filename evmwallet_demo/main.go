// Demo bootstrap for the C-chain-with-any-EVM-wallet prototype: starts a
// persistent tmpnet network with SAE from genesis and writes everything the
// demo page needs to os.Args[2]. The network keeps running after this exits.
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/crosschain"
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

	ethClient, err := ethclient.Dial(uri + "/ext/bc/C/rpc")
	check(err)
	cChainID, err := ethClient.ChainID(ctx)
	check(err)

	hrp := constants.GetHRP(networkID)
	pAddress, err := address.Format("P", hrp, key.Address().Bytes())
	check(err)
	out := map[string]any{
		"uri":                uri,
		"networkID":          networkID,
		"hrp":                hrp,
		"ethChainID":         cChainID.String(),
		"precompile":         crosschain.ContractAddress.Hex(),
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
