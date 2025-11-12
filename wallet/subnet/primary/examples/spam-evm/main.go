// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"log"
	"math/big"
	"time"

	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

const (
	// maxFeePerGas is the maximum fee that transactions issued by this test
	// will be willing to pay. The actual value doesn't really matter, it
	// just needs to be higher than the `targetGasPrice` calculated below.
	maxFeePerGas = 1000 * params.GWei
	// minFeePerGas is the minimum fee that transactions issued by this test
	// will pay. The mempool enforces that this value is non-zero.
	minFeePerGas = 1 * params.Wei
)

var (
	gasFeeCap = big.NewInt(maxFeePerGas)
	gasTipCap = big.NewInt(minFeePerGas)
)

func main() {
	ctx := context.Background()
	const (
		chainUUID = "C"
		uri       = primary.LocalAPIURI + "/ext/bc/" + chainUUID + "/sae/http"
	)
	c, err := ethclient.DialContext(ctx, uri)
	if err != nil {
		log.Fatal(err)
	}

	chainID, err := c.ChainID(ctx)
	if err != nil {
		log.Fatal(err)
	}
	signer := types.NewLondonSigner(chainID)

	key := genesis.EWOQKey
	ecdsaKey := key.ToECDSA()
	eoa := crypto.PubkeyToAddress(ecdsaKey.PublicKey)
	nonce, err := c.NonceAt(context.Background(), eoa, nil)
	if err != nil {
		log.Fatal(err)
	}

	for {
		tx := types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			GasPrice: gasFeeCap,
			Gas:      1_000_000, // params.TxGas,
			To:       &eoa,
		})

		tx, err = types.SignTx(tx, signer, ecdsaKey)
		if err != nil {
			log.Fatal(err)
		}

		txHash := tx.Hash()
		log.Printf("sending tx %s with nonce %d\n", txHash, nonce)

		err = c.SendTransaction(ctx, tx)
		if err != nil {
			log.Fatal(err)
		}

		for {
			_, err = c.TransactionReceipt(ctx, txHash)
			if err == nil {
				break // Transaction was confirmed
			}
			if !errors.Is(err, ethereum.NotFound) {
				log.Fatal(err) // Unexpected error
			}

			time.Sleep(100 * time.Millisecond)
		}

		nonce++
	}
}
