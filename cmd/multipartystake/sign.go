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

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func signTxCmd() *cobra.Command {
	var (
		txFilePath string
		keyHex     string
		outputPath string
	)

	cmd := &cobra.Command{
		Use:   "sign-tx",
		Short: "Sign the multi-party AddPermissionlessValidatorTx with a private key",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSign(txFilePath, keyHex, outputPath)
		},
	}
	cmd.Flags().StringVar(&txFilePath, "tx-file", "", "Path to the prepare output JSON")
	cmd.Flags().StringVar(&keyHex, "key", "", "Private key as hex string")
	cmd.Flags().StringVar(&outputPath, "output", "sig.json", "Path to partial signature output")
	_ = cmd.MarkFlagRequired("tx-file")
	_ = cmd.MarkFlagRequired("key")
	return cmd
}

func runSign(txFilePath, keyHex, outputPath string) error {
	// Load prepare output
	prepareBytes, err := os.ReadFile(txFilePath)
	if err != nil {
		return fmt.Errorf("reading tx file: %w", err)
	}

	var prepareOutput PrepareOutput
	if err := json.Unmarshal(prepareBytes, &prepareOutput); err != nil {
		return fmt.Errorf("parsing tx file: %w", err)
	}

	// Parse the private key (PrivateKey-<cb58> format)
	var privKey secp256k1.PrivateKey
	if err := privKey.UnmarshalText([]byte(keyHex)); err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}

	// Deserialize unsigned tx
	unsignedBytes, err := hex.DecodeString(prepareOutput.UnsignedTxHex)
	if err != nil {
		return fmt.Errorf("decoding unsigned tx hex: %w", err)
	}

	utx, err := parseUnsignedTx(unsignedBytes)
	if err != nil {
		return fmt.Errorf("parsing unsigned tx: %w", err)
	}

	// Reconstruct a minimal UTXO backend for signing from the embedded UTXOs JSON
	utxoBackend, err := buildUTXOBackend(prepareOutput.UTXOs)
	if err != nil {
		return fmt.Errorf("building UTXO backend: %w", err)
	}

	// Create keychain and signer
	kc := secp256k1fx.NewKeychain(&privKey)
	txSigner := signer.New(kc, utxoBackend)

	ctx := context.Background()
	if err := txSigner.Sign(ctx, utx); err != nil {
		return fmt.Errorf("signing tx: %w", err)
	}

	// Extract all credentials — the signer only populates slots it can sign,
	// leaving the rest as zero-valued.
	creds := make([]CredData, len(utx.Creds))
	signedCount := 0
	for i, credIntf := range utx.Creds {
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
		return fmt.Errorf("private key did not match any input in the transaction")
	}

	myAddr := privKey.Address()
	networkID := utx.Unsigned.(*txs.AddPermissionlessValidatorTx).NetworkID
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

	fmt.Printf("Signed %d inputs, written to %s\n", signedCount, outputPath)
	return nil
}

// parseUnsignedTx deserializes raw unsigned tx bytes into a Tx.
func parseUnsignedTx(unsignedBytes []byte) (*txs.Tx, error) {
	var utx txs.UnsignedTx
	if _, err := txs.Codec.Unmarshal(unsignedBytes, &utx); err != nil {
		return nil, fmt.Errorf("unmarshaling unsigned tx: %w", err)
	}

	tx := &txs.Tx{
		Unsigned: utx,
	}
	tx.SetBytes(unsignedBytes, unsignedBytes)
	return tx, nil
}

// buildUTXOBackend creates a minimal in-memory UTXO backend from codec-hex-encoded UTXOs.
func buildUTXOBackend(utxoHexes []string) (signer.Backend, error) {
	utxos := common.NewUTXOs()
	ctx := context.Background()

	for i, utxoHex := range utxoHexes {
		utxoBytes, err := hex.DecodeString(utxoHex)
		if err != nil {
			return nil, fmt.Errorf("decoding UTXO %d hex: %w", i, err)
		}
		var utxo avax.UTXO
		if _, err := txs.Codec.Unmarshal(utxoBytes, &utxo); err != nil {
			return nil, fmt.Errorf("unmarshaling UTXO %d: %w", i, err)
		}
		if err := utxos.AddUTXO(ctx, constants.PlatformChainID, constants.PlatformChainID, &utxo); err != nil {
			return nil, fmt.Errorf("adding UTXO: %w", err)
		}
	}

	chainUTXOs := common.NewChainUTXOs(constants.PlatformChainID, utxos)
	return &minimalSignerBackend{utxos: chainUTXOs}, nil
}

// minimalSignerBackend implements signer.Backend using in-memory UTXOs.
type minimalSignerBackend struct {
	utxos common.ChainUTXOs
}

func (b *minimalSignerBackend) GetUTXO(ctx context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error) {
	return b.utxos.GetUTXO(ctx, chainID, utxoID)
}

func (b *minimalSignerBackend) GetOwner(_ context.Context, _ ids.ID) (fx.Owner, error) {
	return nil, fmt.Errorf("GetOwner not supported in offline mode")
}
