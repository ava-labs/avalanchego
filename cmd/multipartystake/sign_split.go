// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/signer"
)

func signSplitCmd() *cobra.Command {
	var (
		splitTxFile string
		keyHex      string
		outputPath  string
	)

	cmd := &cobra.Command{
		Use:   "sign-split",
		Short: "Sign the reward-split BaseTx with a private key",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSignSplit(splitTxFile, keyHex, outputPath)
		},
	}
	cmd.Flags().StringVar(&splitTxFile, "tx-file", "", "Path to the prepare-split output JSON")
	cmd.Flags().StringVar(&keyHex, "key", "", "Private key as hex string")
	cmd.Flags().StringVar(&outputPath, "output", "split-sig.json", "Path to partial signature output")
	_ = cmd.MarkFlagRequired("tx-file")
	_ = cmd.MarkFlagRequired("key")
	return cmd
}

func runSignSplit(splitTxFile, keyHex, outputPath string) error {
	// Load prepare-split output
	splitBytes, err := os.ReadFile(splitTxFile)
	if err != nil {
		return fmt.Errorf("reading split tx file: %w", err)
	}

	var splitOutput SplitPrepareOutput
	if err := json.Unmarshal(splitBytes, &splitOutput); err != nil {
		return fmt.Errorf("parsing split tx file: %w", err)
	}

	// Parse the private key (PrivateKey-<cb58> format)
	var privKey secp256k1.PrivateKey
	if err := privKey.UnmarshalText([]byte(keyHex)); err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}

	// Deserialize unsigned tx
	unsignedBytes, err := hex.DecodeString(splitOutput.UnsignedTxHex)
	if err != nil {
		return fmt.Errorf("decoding unsigned tx hex: %w", err)
	}

	tx, err := parseUnsignedTx(unsignedBytes)
	if err != nil {
		return fmt.Errorf("parsing unsigned tx: %w", err)
	}

	// Build UTXO backend from embedded UTXOs
	utxoBackend, err := buildUTXOBackend(splitOutput.UTXOs)
	if err != nil {
		return fmt.Errorf("building UTXO backend: %w", err)
	}

	// Create keychain and signer
	kc := secp256k1fx.NewKeychain(&privKey)
	txSigner := signer.New(kc, utxoBackend)

	ctx := context.Background()
	if err := txSigner.Sign(ctx, tx); err != nil {
		return fmt.Errorf("signing tx: %w", err)
	}

	// Extract credentials
	creds := make([]CredData, len(tx.Creds))
	signedCount := 0
	for i, credIntf := range tx.Creds {
		cred, ok := credIntf.(*secp256k1fx.Credential)
		if !ok {
			return fmt.Errorf("credential %d is not secp256k1fx.Credential", i)
		}
		sigs := make([]string, len(cred.Sigs))
		hasSignature := false
		for j, sig := range cred.Sigs {
			sigs[j] = hex.EncodeToString(sig[:])
			var empty [secp256k1.SignatureLen]byte
			if sig != empty {
				hasSignature = true
			}
		}
		if hasSignature {
			signedCount++
		}
		creds[i] = CredData{Signatures: sigs}
	}

	if signedCount == 0 {
		return fmt.Errorf("private key did not match any input in the split transaction")
	}

	myAddr := privKey.Address()
	networkID := tx.Unsigned.(*txs.BaseTx).NetworkID
	hrp := constants.GetHRP(networkID)
	addrStr, err := address.Format("P", hrp, myAddr[:])
	if err != nil {
		return fmt.Errorf("formatting address: %w", err)
	}
	partialSig := PartialSignature{
		Address:     addrStr,
		Credentials: creds,
	}

	outputBytes, err := json.MarshalIndent(partialSig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling partial signature: %w", err)
	}

	if err := os.WriteFile(outputPath, outputBytes, 0o644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	fmt.Printf("Signed %d inputs for split tx, written to %s\n", signedCount, outputPath)
	return nil
}
