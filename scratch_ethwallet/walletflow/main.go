// Wallet-flow proof for the P-chain eth facade, driven entirely through
// geth's ethclient (the libevm fork of it), which performs the same strict
// JSON-RPC sequence and response validation a wallet stack does: chainId
// probe, block polling with header sanity checks, EIP-1559 fee discovery,
// nonce and gas queries, raw tx submission, receipt and block cross-checks,
// eth_call token reads and log filtering.
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math/big"
	"time"

	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/ethclient"

	ethtypes "github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	network, err := tmpnet.ReadNetwork(ctx, logging.NoLog{}, *networkDir)
	must(err)
	uri := network.Nodes[0].URI
	pClient := platformvm.NewClient(uri)

	client, err := ethclient.Dial(uri + "/ext/bc/P/eth")
	must(err)
	fmt.Printf("ethclient connected to %s/ext/bc/P/eth\n", uri)

	senderKey, err := secp256k1.NewPrivateKey()
	must(err)
	sender := ethcommon.Address(senderKey.PublicKey().EthAddress())
	recipient := ethcommon.Address(ids.GenerateTestShortID())

	// Fund via V1.
	kc := secp256k1fx.NewKeychain(network.PreFundedKeys[0])
	wallet, err := primary.MakeWallet(ctx, uri, kc, kc, primary.WalletConfig{})
	must(err)
	_, err = wallet.P().IssueBaseTx([]*avax.TransferableOutput{{
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

	// Wallet probes, all through ethclient.
	chainID, err := client.ChainID(ctx)
	must(err)
	blockNumber, err := client.BlockNumber(ctx)
	must(err)
	latest, err := client.BlockByNumber(ctx, nil) // strict header decode
	must(err)
	gasPrice, err := client.SuggestGasPrice(ctx)
	must(err)
	tip, err := client.SuggestGasTipCap(ctx)
	must(err)
	balance, err := client.BalanceAt(ctx, sender, nil)
	must(err)
	fmt.Printf("ChainID:          %s\n", chainID)
	fmt.Printf("BlockNumber:      %d\n", blockNumber)
	fmt.Printf("BlockByNumber:    height %s, baseFee %s, %d txs\n",
		latest.Number(), latest.BaseFee(), len(latest.Transactions()))
	fmt.Printf("SuggestGasPrice:  %s wei\n", gasPrice)
	fmt.Printf("SuggestGasTipCap: %s wei\n", tip)
	fmt.Printf("BalanceAt:        %s wei\n\n", balance)

	// Plain transfer.
	fmt.Println("== transfer")
	receipt := sendViaClient(ctx, client, senderKey, recipient, navax(3*units.Avax), nil)
	crossCheckBlock(ctx, client, receipt)

	// Delegate.
	fmt.Println("\n== delegate")
	validators, err := pClient.GetCurrentValidators(ctx, constants.PrimaryNetworkID, nil)
	must(err)
	target := validators[0]
	calldata := delegateCalldata(target.NodeID, target.EndTime)
	const stake = 100 * units.Avax
	receipt = sendViaClient(ctx, client, senderKey, txs.EthStakingAddress, navax(stake), calldata)
	crossCheckBlock(ctx, client, receipt)
	if len(receipt.Logs) != 1 {
		log.Fatalf("expected one staked-token mint log, got %d", len(receipt.Logs))
	}
	mint := receipt.Logs[0]
	fmt.Printf("mint log:          %s Transfer(0x0 -> %s, %s)\n",
		mint.Address, ethcommon.BytesToAddress(mint.Topics[2][12:]),
		new(big.Int).SetBytes(mint.Data))

	// Token reads via CallContract.
	balanceOf := append(ethcommon.FromHex("0x70a08231"), ethcommon.LeftPadBytes(sender.Bytes(), 32)...)
	stBal, err := client.CallContract(ctx, ethereum.CallMsg{
		To: &txs.EthStakedAVAXAddress, Data: balanceOf,
	}, nil)
	must(err)
	supply, err := client.CallContract(ctx, ethereum.CallMsg{
		To: &txs.EthStakedAVAXAddress, Data: ethcommon.FromHex("0x18160ddd"),
	}, nil)
	must(err)
	fmt.Printf("staked balanceOf:  %s wei (staked %s)\n", new(big.Int).SetBytes(stBal), navax(stake))
	fmt.Printf("STAKED totalSupply: %s wei\n", new(big.Int).SetBytes(supply))

	// Log filtering via FilterLogs.
	logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: big.NewInt(0),
		Addresses: []ethcommon.Address{txs.EthStakedAVAXAddress},
		Topics:    [][]ethcommon.Hash{nil, nil, {topicOf(sender)}},
	})
	must(err)
	fmt.Printf("FilterLogs:        %d log(s), tx %s\n", len(logs), logs[0].TxHash)

	// Native cross-check.
	validators, err = pClient.GetCurrentValidators(ctx, constants.PrimaryNetworkID, []ids.NodeID{target.NodeID})
	must(err)
	delegator := validators[0].Delegators[0]
	fmt.Printf("\nplatform.getCurrentValidators: delegator %d nAVAX, reward owner %s (eth-derived: %v)\n",
		delegator.Weight, delegator.RewardOwner.Addresses[0],
		delegator.RewardOwner.Addresses[0] == ids.ShortID(sender))
}

func sendViaClient(
	ctx context.Context,
	client *ethclient.Client,
	key *secp256k1.PrivateKey,
	to ethcommon.Address,
	value *big.Int,
	calldata []byte,
) *ethtypes.Receipt {
	sender := ethcommon.Address(key.PublicKey().EthAddress())

	nonce, err := client.PendingNonceAt(ctx, sender)
	must(err)
	gasPrice, err := client.SuggestGasPrice(ctx)
	must(err)
	gas, err := client.EstimateGas(ctx, ethereum.CallMsg{
		From: sender, To: &to, Value: value, Data: calldata,
	})
	must(err)

	chainID, err := client.ChainID(ctx)
	must(err)
	signed, err := ethtypes.SignNewTx(
		key.ToECDSA(),
		ethtypes.LatestSignerForChainID(chainID),
		&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: big.NewInt(0),
			GasFeeCap: gasPrice,
			Gas:       gas,
			To:        &to,
			Value:     value,
			Data:      calldata,
		},
	)
	must(err)
	must(client.SendTransaction(ctx, signed))
	fmt.Printf("SendTransaction:   %s (nonce %d, gas %d)\n", signed.Hash(), nonce, gas)

	// Poll TransactionByHash until mined, then the receipt, like wallets do.
	for {
		_, pending, err := client.TransactionByHash(ctx, signed.Hash())
		must(err)
		if !pending {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	receipt, err := client.TransactionReceipt(ctx, signed.Hash())
	must(err)
	fmt.Printf("Receipt:           block %d, status %d, gasUsed %d (signed %d), fee %s wei\n",
		receipt.BlockNumber, receipt.Status, receipt.GasUsed, gas,
		new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), receipt.EffectiveGasPrice))
	return receipt
}

// crossCheckBlock verifies a wallet-visible invariant: the receipt's block
// resolves by both number and hash and contains the tx.
func crossCheckBlock(ctx context.Context, client *ethclient.Client, receipt *ethtypes.Receipt) {
	byNumber, err := client.BlockByNumber(ctx, receipt.BlockNumber)
	must(err)
	byHash, err := client.BlockByHash(ctx, receipt.BlockHash)
	must(err)
	if byNumber.Hash() != byHash.Hash() {
		log.Fatal("block views disagree")
	}
	found := false
	for _, tx := range byHash.Transactions() {
		if tx.Hash() == receipt.TxHash {
			found = true
		}
	}
	fmt.Printf("Block cross-check: byNumber == byHash, contains tx: %v\n", found)
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

func topicOf(addr ethcommon.Address) ethcommon.Hash {
	var topic ethcommon.Hash
	copy(topic[12:], addr[:])
	return topic
}

func navax(amount uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(amount), big.NewInt(1_000_000_000))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
