// Demo "stock EVM wallet" for the P-chain eth facade prototype.
//
// Phase 1 funds a fresh eth-derived address on the P-chain with a V1 BaseTx
// from a tmpnet pre-funded key. Phase 2 signs a plain type-2 EVM transfer with
// libevm (byte-identical to what MetaMask produces) and drives it through the
// facade RPC: nonce, gas price, estimate, sendRawTransaction, receipt poll.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	ethtypes "github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"

	ethcommon "github.com/ava-labs/libevm/common"
)

func main() {
	networkDir := flag.String("network-dir", "", "tmpnet network dir")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	network, err := tmpnet.ReadNetwork(ctx, logging.NoLog{}, *networkDir)
	must(err)
	uri := network.Nodes[0].URI
	ethRPC := uri + "/ext/bc/P/eth"
	fmt.Printf("node URI:    %s\n", uri)

	// The "wallet" keys: a fresh sender and recipient, EVM-style.
	senderKey, err := secp256k1.NewPrivateKey()
	must(err)
	sender := ethcommon.Address(senderKey.PublicKey().EthAddress())
	recipientKey, err := secp256k1.NewPrivateKey()
	must(err)
	recipient := ethcommon.Address(recipientKey.PublicKey().EthAddress())
	fmt.Printf("sender:      %s\nrecipient:   %s\n", sender, recipient)

	// Phase 1: V1 funding of the sender's eth address (10 AVAX).
	kc := secp256k1fx.NewKeychain(network.PreFundedKeys[0])
	wallet, err := primary.MakeWallet(ctx, uri, kc, kc, primary.WalletConfig{})
	must(err)
	fundTx, err := wallet.P().IssueBaseTx([]*avax.TransferableOutput{{
		Asset: avax.Asset{ID: wallet.P().Builder().Context().AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 10 * units.Avax,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.ShortID(sender)},
			},
		},
	}})
	must(err)
	fmt.Printf("V1 funding tx accepted: %s\n\n", fundTx.ID())

	// Phase 2: the stock EVM flow against the facade.
	chainIDHex := rpcCall(ethRPC, "eth_chainId")
	nonceHex := rpcCall(ethRPC, "eth_getTransactionCount", sender.Hex(), "latest")
	gasPriceHex := rpcCall(ethRPC, "eth_gasPrice")
	gasHex := rpcCall(ethRPC, "eth_estimateGas", map[string]string{"from": sender.Hex(), "to": recipient.Hex()})
	balHex := rpcCall(ethRPC, "eth_getBalance", sender.Hex(), "latest")
	fmt.Printf("eth_chainId:             %s\n", chainIDHex)
	fmt.Printf("eth_getTransactionCount: %s\n", nonceHex)
	fmt.Printf("eth_gasPrice:            %s\n", gasPriceHex)
	fmt.Printf("eth_estimateGas:         %s\n", gasHex)
	fmt.Printf("eth_getBalance(sender):  %s (%s AVAX)\n", balHex, weiToAvax(balHex))

	chainID := hexBig(chainIDHex)
	signed := ethtypes.MustSignNewTx(
		senderKey.ToECDSA(),
		ethtypes.LatestSignerForChainID(chainID),
		&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     hexBig(nonceHex).Uint64(),
			GasTipCap: big.NewInt(0),
			GasFeeCap: hexBig(gasPriceHex),
			Gas:       hexBig(gasHex).Uint64(),
			To:        &recipient,
			Value:     new(big.Int).Mul(big.NewInt(3), big.NewInt(1e18)), // 3 AVAX
		},
	)
	raw, err := signed.MarshalBinary()
	must(err)
	fmt.Printf("\nsigned type-2 eth tx:    %s (%d bytes RLP)\n", signed.Hash(), len(raw))

	txHash := rpcCall(ethRPC, "eth_sendRawTransaction", fmt.Sprintf("0x%x", raw))
	fmt.Printf("eth_sendRawTransaction:  %s\n", txHash)

	// The avalanchego-side txID, so failures can be read off platform.getTxStatus.
	pTx, err := txs.NewSigned(&txs.EthRLPTx{RLP: raw}, txs.Codec, nil)
	must(err)
	pClient := platformvm.NewClient(uri)

	var receipt map[string]any
	for i := 0; i < 100; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := rpcCallInto(ethRPC, &receipt, "eth_getTransactionReceipt", txHash); err == nil && receipt != nil {
			break
		}
	}
	if receipt == nil {
		st, stErr := pClient.GetTxStatus(ctx, pTx.ID())
		log.Fatalf("no receipt after 10s; P-chain txID %s status=%+v err=%v", pTx.ID(), st, stErr)
	}
	pretty, _ := json.MarshalIndent(receipt, "", "  ")
	fmt.Printf("eth_getTransactionReceipt:\n%s\n\n", pretty)

	senderBal := rpcCall(ethRPC, "eth_getBalance", sender.Hex(), "latest")
	recipientBal := rpcCall(ethRPC, "eth_getBalance", recipient.Hex(), "latest")
	nonceAfter := rpcCall(ethRPC, "eth_getTransactionCount", sender.Hex(), "latest")
	fmt.Printf("eth_getBalance(sender):    %s (%s AVAX)\n", senderBal, weiToAvax(senderBal))
	fmt.Printf("eth_getBalance(recipient): %s (%s AVAX)\n", recipientBal, weiToAvax(recipientBal))
	fmt.Printf("eth_getTransactionCount:   %s\n", nonceAfter)

	// Cross-check on the V1 side: the recipient's UTXO exists on the P-chain.
	utxos, _, _, err := platformvm.NewClient(uri).GetUTXOs(
		ctx, []ids.ShortID{ids.ShortID(recipient)}, 100, ids.ShortEmpty, ids.Empty)
	must(err)
	fmt.Printf("V1 view: recipient owns %d P-chain UTXO(s)\n", len(utxos))
	_ = txs.EthRLPChainID
}

func weiToAvax(hexWei string) string {
	wei := hexBig(hexWei)
	avaxWhole := new(big.Int).Div(wei, big.NewInt(1e18))
	frac := new(big.Int).Mod(new(big.Int).Div(wei, big.NewInt(1e9)), big.NewInt(1e9))
	return fmt.Sprintf("%s.%09d", avaxWhole, frac)
}

func hexBig(s string) *big.Int {
	v, ok := new(big.Int).SetString(s[2:], 16)
	if !ok {
		log.Fatalf("bad hex %q", s)
	}
	return v
}

func rpcCall(url, method string, params ...any) string {
	var result string
	if err := rpcCallInto(url, &result, method, params...); err != nil {
		log.Fatalf("%s: %v", method, err)
	}
	return result
}

func rpcCallInto(url string, result any, method string, params ...any) error {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return err
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("rpc error: %s", rpcResp.Error.Message)
	}
	if string(rpcResp.Result) == "null" {
		return nil
	}
	return json.Unmarshal(rpcResp.Result, result)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
