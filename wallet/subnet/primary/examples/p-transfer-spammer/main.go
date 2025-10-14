// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func main() {
	var (
		uri        string
		fromKeyStr string
		toStr      string
		amount     uint64
		intervalMs int
		assume     bool
	)

	flag.StringVar(&uri, "uri", primary.LocalAPIURI, "RPC endpoint, e.g. http://127.0.0.1:9650")
	flag.StringVar(&fromKeyStr, "from-key", "", "secp256k1 private key (hex or PrivateKey-...) for the sender")
	flag.StringVar(&toStr, "to", "", "destination address (P-... bech32 or shortID)")
	flag.Uint64Var(&amount, "amount", units.Schmeckle, "amount in nAVAX to send each tx")
	flag.IntVar(&intervalMs, "interval-ms", 500, "interval between txs in milliseconds")
	flag.BoolVar(&assume, "assume-decided", true, "issue without waiting for acceptance confirmation")
	flag.Parse()

	if fromKeyStr == "" || toStr == "" {
		fmt.Fprintf(os.Stderr, "missing required flags: --from-key and --to\n")
		flag.PrintDefaults()
		os.Exit(2)
	}

	// Parse sender key via hex string (with or without 0x prefix)
	var sk secp256k1.PrivateKey
	keyHex := strings.TrimSpace(fromKeyStr)
	if len(keyHex) > 2 && (strings.HasPrefix(keyHex, "0x") || strings.HasPrefix(keyHex, "0X")) {
		keyHex = keyHex[2:]
	}
	skBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		log.Fatalf("failed to decode private key hex: %v", err)
	}
	skPtr, err := secp256k1.ToPrivateKey(skBytes)
	if err != nil {
		log.Fatalf("failed to create private key: %v", err)
	}
	sk = *skPtr

	// Parse destination address to ids.ShortID using general parser
	var toAddr ids.ShortID
	if _, _, addrBytes, err := address.Parse(toStr); err == nil {
		// Parsed as bech32 (e.g., P-avax1...)
		parsed, err := ids.ToShortID(addrBytes)
		if err != nil {
			log.Fatalf("failed to convert destination address: %v", err)
		}
		toAddr = parsed
	} else {
		// Fallback: treat as short (cb58) address
		parsed, err := ids.ShortFromString(toStr)
		if err != nil {
			log.Fatalf("failed to parse destination address: %v", err)
		}
		toAddr = parsed
	}

	// Build owner for destination
	toOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			toAddr,
		},
	}

	// Initialize keychain with sender key
	kc := secp256k1fx.NewKeychain(&sk)

	ctx := context.Background()

	// Create a P-chain capable wallet synced to this key
	walletSyncStart := time.Now()
	pWallet, err := primary.MakePWallet(
		ctx,
		uri,
		kc,
		primary.WalletConfig{},
	)
	if err != nil {
		log.Fatalf("failed to initialize wallet: %v", err)
	}
	log.Printf("synced wallet in %s", time.Since(walletSyncStart))

	// Pull AVAX asset ID and sanity-check balance once
	pBuilder := pWallet.Builder()
	pCtx := pBuilder.Context()

	// Set up graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	log.Printf("starting P-chain transfers: from=%s to=%s amount=%d nAVAX every %dms", sk.PublicKey().Address(), toStr, amount, intervalMs)

	// Prepare static options
	var opts []common.Option
	if assume {
		opts = append(opts, common.WithAssumeDecided())
	}

	// Loop until interrupted
	for i := 0; ; i++ {
		select {
		case <-stop:
			log.Printf("stopping after %d attempts", i)
			return
		case <-ticker.C:
			// Ensure there is enough for fee + amount
			balances, err := pBuilder.GetBalance()
			if err != nil {
				log.Printf("balance query failed: %v", err)
				continue
			}
			avaxBal := balances[pCtx.AVAXAssetID]
			// very rough upper bound: require amount + 1e5 nAVAX for fees
			const feeHeadroom = 100_000
			if avaxBal < amount+feeHeadroom {
				log.Printf("insufficient balance: have=%d need>=%d", avaxBal, amount+feeHeadroom)
				continue
			}

			outputs := []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: pCtx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          amount,
						OutputOwners: *toOwner,
					},
				},
			}

			start := time.Now()
			tx, err := pWallet.IssueBaseTx(outputs, opts...)
			if err != nil {
				log.Printf("issue base tx failed: %v", err)
				continue
			}
			dur := time.Since(start)
			log.Printf("issued P-chain base tx %s in %s", tx.ID(), dur)
		}
	}
}
