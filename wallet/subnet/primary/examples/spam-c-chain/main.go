// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"log"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"

	ethereum "github.com/ava-labs/libevm"
)

// maxFeePerGas is the fee that transactions issued by this test will pay.
const maxFeePerGas = 1000 * params.GWei

var gasPrice = big.NewInt(maxFeePerGas)

func main() {
	ctx := context.Background()
	const (
		chainUUID = "C"
		uri       = primary.LocalAPIURI + "/ext/bc/" + chainUUID + "/rpc"
	)
	c, err := ethclient.DialContext(ctx, uri)
	if err != nil {
		log.Fatal(err)
	}

	chainID, err := c.ChainID(ctx)
	if err != nil {
		log.Fatal("chainID", err)
	}
	signer := types.NewLondonSigner(chainID)

	key := genesis.EWOQKey
	ecdsaKey := key.ToECDSA()
	eoa := crypto.PubkeyToAddress(ecdsaKey.PublicKey)
	nonce, err := c.NonceAt(context.Background(), eoa, nil)
	if err != nil {
		log.Fatal("nonceAt", err)
	}

	for {
		tx := types.NewTx(&types.DynamicFeeTx{
			Nonce:     nonce,
			GasTipCap: gasPrice,
			GasFeeCap: gasPrice,
			Gas:       1_000_000, // params.TxGas,
			To:        &eoa,
		})

		tx, err = types.SignTx(tx, signer, ecdsaKey)
		if err != nil {
			log.Fatal("signTx", err)
		}

		txHash := tx.Hash()
		log.Printf("sending tx %s with nonce %d\n", txHash, nonce)

		err = c.SendTransaction(ctx, tx)
		if err != nil {
			log.Fatal("sendTx", err)
		}

		for {
			_, err = c.TransactionReceipt(ctx, txHash)
			if err == nil {
				break // Transaction was confirmed
			}
			if !errors.Is(err, ethereum.NotFound) {
				log.Fatal("transactionReceipt", err) // Unexpected error
			}

			time.Sleep(100 * time.Millisecond)
		}

		nonce++
	}
}
