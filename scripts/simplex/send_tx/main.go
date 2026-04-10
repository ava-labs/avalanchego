package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
)

func main() {
	nodesFile := flag.String("nodes", "", "Path to nodes.json")
	chainsFile := flag.String("chains", "", "Path to chains.json")
	flag.Parse()

	if *nodesFile == "" || *chainsFile == "" {
		log.Fatal("--nodes and --chains are required")
	}

	// Read node URIs
	nodesData, err := os.ReadFile(*nodesFile)
	if err != nil {
		log.Fatalf("failed to read nodes file: %s", err)
	}
	var nodeURIs []string
	if err := json.Unmarshal(nodesData, &nodeURIs); err != nil {
		log.Fatalf("failed to parse nodes file: %s", err)
	}
	if len(nodeURIs) == 0 {
		log.Fatal("no node URIs found")
	}

	// Read chains to find the simplex L1 chain RPC path
	chainsData, err := os.ReadFile(*chainsFile)
	if err != nil {
		log.Fatalf("failed to read chains file: %s", err)
	}
	type chainEntry struct {
		Name    string `json:"name"`
		ChainID string `json:"chainId"`
		RPCPath string `json:"rpcPath"`
		VM      string `json:"vm"`
	}
	var chains []chainEntry
	if err := json.Unmarshal(chainsData, &chains); err != nil {
		log.Fatalf("failed to parse chains file: %s", err)
	}

	var rpcURL string
	for _, c := range chains {
		if c.VM == "subnet-evm" {
			rpcURL = nodeURIs[0] + c.RPCPath
			break
		}
	}
	if rpcURL == "" {
		log.Fatal("no subnet-evm chain found in chains.json")
	}

	log.Printf("RPC URL: %s", rpcURL)

	// Ewoq private key (pre-funded in genesis)
	ewoqKeyHex := "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"
	privateKey, err := crypto.HexToECDSA(ewoqKeyHex)
	if err != nil {
		log.Fatalf("failed to parse private key: %s", err)
	}
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	log.Printf("From address: %s", fromAddress.Hex())

	// Connect to the RPC endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		log.Fatalf("failed to connect to RPC: %s", err)
	}
	defer client.Close()

	// Check chain ID
	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Fatalf("failed to get chain ID: %s", err)
	}
	log.Printf("Chain ID: %s", chainID)

	// Check balance
	balance, err := client.BalanceAt(ctx, fromAddress, nil)
	if err != nil {
		log.Fatalf("failed to get balance: %s", err)
	}
	log.Printf("Balance: %s wei", balance)

	// Check current block number
	blockNum, err := client.BlockNumber(ctx)
	if err != nil {
		log.Fatalf("failed to get block number: %s", err)
	}
	log.Printf("Current block number: %d", blockNum)

	// Get nonce
	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		log.Fatalf("failed to get nonce: %s", err)
	}
	log.Printf("Nonce: %d", nonce)

	// Get gas price
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		log.Fatalf("failed to get gas price: %s", err)
	}
	log.Printf("Gas price: %s", gasPrice)

	// Send a simple self-transfer of 0 AVAX to trigger a pending tx event
	toAddress := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	value := big.NewInt(1e18) // 1 AVAX

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &toAddress,
		Value:    value,
		Gas:      21000,
		GasPrice: gasPrice,
		Data:     nil,
	})

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		log.Fatalf("failed to sign tx: %s", err)
	}
	log.Printf("Signed tx hash: %s", signedTx.Hash().Hex())

	// Send the transaction
	log.Println("Sending transaction...")
	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Fatalf("failed to send transaction: %s", err)
	}
	log.Printf("Transaction sent! Hash: %s", signedTx.Hash().Hex())

	// Wait for the receipt
	log.Println("Waiting for receipt...")
	for i := 0; i < 30; i++ {
		receipt, err := client.TransactionReceipt(ctx, signedTx.Hash())
		if err == nil {
			log.Printf("Transaction mined in block %d, status: %d", receipt.BlockNumber, receipt.Status)
			// Check new block number
			newBlockNum, _ := client.BlockNumber(ctx)
			log.Printf("New block number: %d", newBlockNum)
			return
		}
		fmt.Print(".")
		time.Sleep(1 * time.Second)
	}
	log.Println("\nTransaction not mined within 30 seconds")

	// Check pending nonce to see if tx is in the pool
	pendingNonce, _ := client.PendingNonceAt(ctx, fromAddress)
	log.Printf("Pending nonce: %d (was %d)", pendingNonce, nonce)

	_ = privateKey.Public().(*ecdsa.PublicKey)
}
