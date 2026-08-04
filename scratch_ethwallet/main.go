// Demo "stock EVM wallet" for the P-chain eth facade.
//
// Phase 1 funds a fresh eth-derived address on the P-chain with a V1 BaseTx
// from a tmpnet pre-funded key. Phase 2 signs a plain type-2 EVM transfer with
// libevm (byte-identical to what MetaMask produces) and drives it through the
// facade RPC. Phase 3 delegates to a devnet validator with a staking call and
// checks the result through the native platform API.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
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
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"

	ethcommon "github.com/ava-labs/libevm/common"
)

func main() {
	networkDir := flag.String("network-dir", "", "tmpnet network dir")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	network, err := tmpnet.ReadNetwork(ctx, logging.NoLog{}, *networkDir)
	must(err)
	uri := network.Nodes[0].URI
	ethRPC := uri + "/ext/bc/P/eth"
	pClient := platformvm.NewClient(uri)
	fmt.Printf("node URI:    %s\n", uri)

	senderKey, err := secp256k1.NewPrivateKey()
	must(err)
	sender := ethcommon.Address(senderKey.PublicKey().EthAddress())
	recipientKey, err := secp256k1.NewPrivateKey()
	must(err)
	recipient := ethcommon.Address(recipientKey.PublicKey().EthAddress())
	fmt.Printf("sender:      %s\nrecipient:   %s\n", sender, recipient)

	// Phase 1: V1 funding of the sender's eth address.
	kc := secp256k1fx.NewKeychain(network.PreFundedKeys[0])
	wallet, err := primary.MakeWallet(ctx, uri, kc, kc, primary.WalletConfig{})
	must(err)
	fundTx, err := wallet.P().IssueBaseTx([]*avax.TransferableOutput{{
		Asset: avax.Asset{ID: wallet.P().Builder().Context().AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 500 * units.Avax,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.ShortID(sender)},
			},
		},
	}})
	must(err)
	fmt.Printf("V1 funding tx accepted: %s\n\n", fundTx.ID())

	// Phase 2: plain transfer through the facade.
	fmt.Println("== phase 2: plain transfer")
	gasPriceHex := rpcCall(ethRPC, "eth_gasPrice")
	transferGasHex := rpcCall(ethRPC, "eth_estimateGas",
		map[string]string{"from": sender.Hex(), "to": recipient.Hex()})
	fmt.Printf("eth_chainId:               %s\n", rpcCall(ethRPC, "eth_chainId"))
	fmt.Printf("eth_gasPrice:              %s wei per gas\n", gasPriceHex)
	fmt.Printf("eth_estimateGas:           %s (%d gas)\n", transferGasHex, hexBig(transferGasHex))
	fmt.Printf("eth_getBalance(sender):    %s AVAX\n", balanceAVAX(ethRPC, sender))

	balanceBefore := hexBig(rpcCall(ethRPC, "eth_getBalance", sender.Hex(), "latest"))
	transferHash, transferReceipt := sendAndWait(ethRPC, senderKey, ethTx{
		nonce:  nonceOf(ethRPC, sender),
		to:     recipient,
		value:  navax(3 * units.Avax),
		gas:    hexBig(transferGasHex).Uint64(),
		feeCap: hexBig(gasPriceHex),
	})
	fmt.Printf("eth_sendRawTransaction:    %s\n", transferHash)
	fmt.Printf("receipt:                   block %s, gasUsed %s, status %s\n",
		transferReceipt["blockNumber"], transferReceipt["gasUsed"], transferReceipt["status"])

	// The receipt must name the block the tx is really in.
	receiptHeight := hexBig(transferReceipt["blockNumber"].(string)).Uint64()
	fmt.Printf("tx is in that block:       %v\n", blockContains(ctx, pClient, receiptHeight, transferHash))

	balanceAfter := hexBig(rpcCall(ethRPC, "eth_getBalance", sender.Hex(), "latest"))
	charged := new(big.Int).Sub(new(big.Int).Sub(balanceBefore, balanceAfter), navax(3*units.Avax))
	expected := new(big.Int).Mul(hexBig(transferGasHex), hexBig(gasPriceHex))
	fmt.Printf("fee charged:               %s wei\n", charged)
	fmt.Printf("estimateGas * gasPrice:    %s wei (exact match: %v)\n", expected, charged.Cmp(expected) == 0)
	fmt.Printf("eth_getBalance(sender):    %s AVAX\n", balanceAVAX(ethRPC, sender))
	fmt.Printf("eth_getBalance(recipient): %s AVAX\n\n", balanceAVAX(ethRPC, recipient))

	// Phase 3: delegate to a devnet validator.
	fmt.Println("== phase 3: delegate")
	validators, err := pClient.GetCurrentValidators(ctx, constants.PrimaryNetworkID, nil)
	must(err)
	target := validators[0]
	fmt.Printf("delegating to:             %s (ends %d)\n", target.NodeID, target.EndTime)

	const stake = 100 * units.Avax
	calldata := delegateCalldata(target.NodeID, target.EndTime)
	stakeGasHex := rpcCall(ethRPC, "eth_estimateGas", map[string]string{
		"from": sender.Hex(),
		"to":   txs.EthStakingAddress.Hex(),
		"data": fmt.Sprintf("0x%x", calldata),
	})
	fmt.Printf("eth_estimateGas:           %s (%d gas, includes calldata)\n", stakeGasHex, hexBig(stakeGasHex))

	stakeBefore := hexBig(rpcCall(ethRPC, "eth_getBalance", sender.Hex(), "latest"))
	stakeHash, stakeReceipt := sendAndWait(ethRPC, senderKey, ethTx{
		nonce:  nonceOf(ethRPC, sender),
		to:     txs.EthStakingAddress,
		value:  navax(stake),
		gas:    hexBig(stakeGasHex).Uint64(),
		feeCap: hexBig(rpcCall(ethRPC, "eth_gasPrice")),
		data:   calldata,
	})
	fmt.Printf("eth_sendRawTransaction:    %s\n", stakeHash)
	fmt.Printf("receipt:                   block %s, gasUsed %s, status %s\n",
		stakeReceipt["blockNumber"], stakeReceipt["gasUsed"], stakeReceipt["status"])
	stakeHeight := hexBig(stakeReceipt["blockNumber"].(string)).Uint64()
	fmt.Printf("tx is in that block:       %v\n", blockContains(ctx, pClient, stakeHeight, stakeHash))

	stakeCharged := new(big.Int).Sub(new(big.Int).Sub(stakeBefore,
		hexBig(rpcCall(ethRPC, "eth_getBalance", sender.Hex(), "latest"))), navax(stake))
	stakeExpected := new(big.Int).Mul(hexBig(stakeGasHex), hexBig(gasPriceHex))
	fmt.Printf("fee charged:               %s wei (exact match: %v)\n",
		stakeCharged, stakeCharged.Cmp(stakeExpected) == 0)

	// The delegator must be visible through the native API, owned by the eth
	// address.
	validators, err = pClient.GetCurrentValidators(ctx, constants.PrimaryNetworkID, []ids.NodeID{target.NodeID})
	must(err)
	if len(validators) != 1 || len(validators[0].Delegators) != 1 {
		log.Fatalf("expected exactly one delegator, got %+v", validators)
	}
	delegator := validators[0].Delegators[0]
	fmt.Println("platform.getCurrentValidators:")
	fmt.Printf("  delegator txID:          %s\n", delegator.TxID)
	fmt.Printf("  weight:                  %d nAVAX (staked %d)\n", delegator.Weight, uint64(stake))
	fmt.Printf("  endTime:                 %d (validator %d)\n", delegator.EndTime, target.EndTime)
	fmt.Printf("  reward owner:            threshold %d, %s\n",
		delegator.RewardOwner.Threshold, delegator.RewardOwner.Addresses[0])
	fmt.Printf("  owner is the eth address: %v\n",
		delegator.RewardOwner.Addresses[0] == ids.ShortID(sender))

	// The staker tx is a native delegator tx, not the eth tx.
	stakerTxBytes, err := pClient.GetTx(ctx, delegator.TxID)
	must(err)
	parsed, err := txs.Parse(txs.Codec, stakerTxBytes)
	must(err)
	txStatus, err := pClient.GetTxStatus(ctx, delegator.TxID)
	must(err)
	fmt.Printf("  staker tx type:          %T (status %s)\n", parsed.Unsigned, txStatus.Status)
	fmt.Printf("  staker txID != eth txID: %v\n", delegator.TxID.String() != stakeHash)

	fmt.Printf("\neth_getBalance(sender):    %s AVAX (100 staked, no longer liquid)\n",
		balanceAVAX(ethRPC, sender))
}

type ethTx struct {
	nonce  uint64
	to     ethcommon.Address
	value  *big.Int
	gas    uint64
	feeCap *big.Int
	data   []byte
}

func sendAndWait(ethRPC string, key *secp256k1.PrivateKey, t ethTx) (string, map[string]any) {
	chainID := big.NewInt(txs.EthRLPChainID)
	signed := ethtypes.MustSignNewTx(
		key.ToECDSA(),
		ethtypes.LatestSignerForChainID(chainID),
		&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     t.nonce,
			GasTipCap: big.NewInt(0),
			GasFeeCap: t.feeCap,
			Gas:       t.gas,
			To:        &t.to,
			Value:     t.value,
			Data:      t.data,
		},
	)
	raw, err := signed.MarshalBinary()
	must(err)

	hash := rpcCall(ethRPC, "eth_sendRawTransaction", fmt.Sprintf("0x%x", raw))
	for i := 0; i < 200; i++ {
		time.Sleep(100 * time.Millisecond)
		var receipt map[string]any
		if err := rpcCallInto(ethRPC, &receipt, "eth_getTransactionReceipt", hash); err == nil && receipt != nil {
			return hash, receipt
		}
	}
	log.Fatalf("no receipt for %s after 20s", hash)
	return "", nil
}

// blockContains reports whether the P-chain block at [height] contains the eth
// tx with hash [ethHash], read through the native block API.
func blockContains(ctx context.Context, client *platformvm.Client, height uint64, ethHash string) bool {
	blockBytes, err := client.GetBlockByHeight(ctx, height)
	must(err)
	blk, err := block.Parse(block.Codec, blockBytes)
	must(err)
	for _, tx := range blk.Txs() {
		ethRLP, ok := tx.Unsigned.(*txs.EthRLPTx)
		if !ok {
			continue
		}
		if err := ethRLP.SyntacticVerify(nil); err != nil {
			continue
		}
		if ethRLP.Parsed.Hash().Hex() == ethHash {
			return true
		}
	}
	return false
}

func delegateCalldata(nodeID ids.NodeID, endTime uint64) []byte {
	calldata := txs.SelectorDelegate[:]
	nodeWord := make([]byte, 32)
	copy(nodeWord, nodeID[:])
	calldata = append(calldata, nodeWord...)
	endWord := make([]byte, 32)
	binary.BigEndian.PutUint64(endWord[24:], endTime)
	return append(calldata, endWord...)
}

func nonceOf(ethRPC string, addr ethcommon.Address) uint64 {
	return hexBig(rpcCall(ethRPC, "eth_getTransactionCount", addr.Hex(), "latest")).Uint64()
}

func balanceAVAX(ethRPC string, addr ethcommon.Address) string {
	wei := hexBig(rpcCall(ethRPC, "eth_getBalance", addr.Hex(), "latest"))
	whole := new(big.Int).Div(wei, big.NewInt(1e18))
	frac := new(big.Int).Mod(new(big.Int).Div(wei, big.NewInt(1e9)), big.NewInt(1e9))
	return fmt.Sprintf("%s.%09d", whole, frac)
}

func navax(amount uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(amount), txs.WeiPerNAVAX)
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
