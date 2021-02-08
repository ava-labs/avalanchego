// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"time"

	"math/big"

	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
)

var (
	key                = "ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"
	prefixedPrivateKey = fmt.Sprintf("PrivateKey-%s", key)
	ipAddr             = "127.0.0.1"
	port               = 9650
	pk                 crypto.PrivateKey
	secpKey            *crypto.PrivateKeySECP256K1R
	ethAddr            common.Address
)

func init() {
	pkBytes, err := formatting.Decode(formatting.CB58, key)
	if err != nil {
		panic(err)
	}
	factory := crypto.FactorySECP256K1R{}
	pk, err = factory.ToPrivateKey(pkBytes)
	secpKey = pk.(*crypto.PrivateKeySECP256K1R)
	ethAddr = evm.GetEthAddress(secpKey)
}

type ethWSAPITestExecutor struct {
	uri            string
	requestTimeout time.Duration
}

// ExecuteTest ...
func (e *ethWSAPITestExecutor) ExecuteTest() error {
	client, err := ethclient.Dial(e.uri)
	if err != nil {
		return fmt.Errorf("Failed to create ethclient: %w", err)
	}
	fmt.Printf("Created ethclient\n")

	ctx := context.Background()

	if err := testSubscription(ctx, client); err != nil {
		return fmt.Errorf("Subscription Test failed: %w", err)
	}

	if err := testHeaderAndBlockCalls(ctx, client, ethAddr); err != nil {
		return fmt.Errorf("HeaderAndBlockCalls Test failed: %w", err)
	}

	return nil
}

type ethRPCAPITestExecutor struct {
	uri            string
	requestTimeout time.Duration
}

// ExecuteTest ...
func (e *ethRPCAPITestExecutor) ExecuteTest() error {
	client, err := ethclient.Dial(e.uri)
	if err != nil {
		return fmt.Errorf("Failed to create ethclient: %w", err)
	}
	fmt.Printf("Created ethclient\n")

	ctx := context.Background()

	if err := testHeaderAndBlockCalls(ctx, client, ethAddr); err != nil {
		return fmt.Errorf("HeaderAndBlockCalls Test failed: %w", err)
	}

	return nil
}

func testSubscription(ctx context.Context, client *ethclient.Client) error {
	headerChan := make(chan *types.Header)
	subscription, err := client.SubscribeNewHead(ctx, headerChan)
	if err != nil {
		return fmt.Errorf("Failed to create subscription: %s", err)
	}
	fmt.Printf("Created subscription: %s\n", subscription)

	suggestedGasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("Failed to get suggested gas price: %s", err)
	}
	fmt.Printf("Suggested gas price: %d\n", suggestedGasPrice.Uint64())

	logChan := make(chan types.Log)
	query := coreth.FilterQuery{
		BlockHash: nil,
		FromBlock: nil,
		ToBlock:   nil,
		Addresses: []common.Address{},
		Topics:    [][]common.Hash{},
	}
	subscription, err = client.SubscribeFilterLogs(ctx, query, logChan)
	if err != nil {
		return fmt.Errorf("Failed to create subscription: %s", err)
	}
	fmt.Printf("Created subscription: %s\n", subscription)

	return nil
}

func testHeaderAndBlockCalls(ctx context.Context, client *ethclient.Client, ethAddr common.Address) error {
	// Test Header and Block ByNumber work for special cases
	for i := 0; i > -3; i-- {
		if err := checkHeaderAndBlocks(ctx, client, i, ethAddr); err != nil {
			return err
		}
	}

	return nil
}

func checkHeaderAndBlocks(ctx context.Context, client *ethclient.Client, i int, ethAddr common.Address) error {
	fmt.Printf("Checking HeaderAndBlocks for i = %d\n", i)

	header1, err := client.HeaderByNumber(ctx, big.NewInt(int64(i)))
	if err != nil {
		return fmt.Errorf("Failed to retrieve HeaderByNumber: %w", err)
	}
	fmt.Printf("HeaderByNumber (Block Number: %d, Block Hash: %s)\n", header1.Number, header1.Hash().Hex())

	originalHash := header1.Hash()
	originalBlockNumber := header1.Number
	if i >= 0 && int(originalBlockNumber.Int64()) != i {
		return fmt.Errorf("Requested block number %d, but found block number %d", i, originalBlockNumber)
	}

	header2, err := client.HeaderByHash(ctx, header1.Hash())
	if err != nil {
		return fmt.Errorf("Failed to retrieve HeaderByHash: %w", err)
	}
	fmt.Printf("HeaderByNumber (Block Number: %d, Block Hash: %s)\n", header2.Number, header2.Hash().Hex())

	if originalHash.Hex() != header2.Hash().Hex() {
		return fmt.Errorf("Expected HeaderByHash with Hash: %s, but found: %s", originalHash.Hex(), header2.Hash().Hex())
	}

	if originalBlockNumber.Cmp(header2.Number) != 0 {
		return fmt.Errorf("Expected HeaderByHash with Number: %d, but found %d", originalBlockNumber, header2.Number)
	}

	block1, err := client.BlockByNumber(ctx, big.NewInt(int64(i)))
	if err != nil {
		return fmt.Errorf("Failed to retrieve BlockByNumber: %w", err)
	}
	header3 := block1.Header()
	fmt.Printf("BlockByNumber (Block Number: %d, Block Hash: %s)\n", header3.Number, header3.Hash().Hex())

	if originalHash.Hex() != header3.Hash().Hex() {
		return fmt.Errorf("Expected HeaderByHash with Hash: %s, but found: %s", originalHash.Hex(), header3.Hash().Hex())
	}

	if originalBlockNumber.Cmp(header3.Number) != 0 {
		return fmt.Errorf("Expected HeaderByHash with Number: %d, but found %d", originalBlockNumber, header3.Number)
	}

	block2, err := client.BlockByHash(ctx, header1.Hash())
	if err != nil {
		return fmt.Errorf("Failed to retrieve BlockByHash: %w", err)
	}
	header4 := block2.Header()
	fmt.Printf("BlockByHash (Block Number: %d, Block Hash: %s)\n", header4.Number, header4.Hash().Hex())
	if originalHash.Hex() != header4.Hash().Hex() {
		return fmt.Errorf("Expected HeaderByHash with Hash: %s, but found: %s", originalHash.Hex(), header4.Hash().Hex())
	}

	if originalBlockNumber.Cmp(header4.Number) != 0 {
		return fmt.Errorf("Expected HeaderByHash with Number: %d, but found %d", originalBlockNumber, header4.Number)
	}

	balance, err := client.BalanceAt(ctx, ethAddr, big.NewInt(int64(i)))
	if err != nil {
		return fmt.Errorf("Failed to get balance: %s", err)
	}
	fmt.Printf("Balance: %d for address: %s\n", balance, ethAddr.Hex())

	return nil
}

func main() {
	wsURI := fmt.Sprintf("ws://%s:%d/ext/bc/C/ws", ipAddr, port)
	wsTest := &ethWSAPITestExecutor{
		uri:            wsURI,
		requestTimeout: 3 * time.Second,
	}
	if err := wsTest.ExecuteTest(); err != nil {
		fmt.Printf("WebSocket Test failed due to %s\n", err)
	} else {
		fmt.Printf("WebSocket Test succeeded!\n")
	}

	rpcURI := fmt.Sprintf("http://%s:%d/ext/bc/C/rpc", ipAddr, port)
	rpcTest := &ethRPCAPITestExecutor{
		uri:            rpcURI,
		requestTimeout: 3 * time.Second,
	}
	if err := rpcTest.ExecuteTest(); err != nil {
		fmt.Printf("RPC Test failed due to %s\n", err)
	} else {
		fmt.Printf("RPC Test succeeded!\n")
	}
}
