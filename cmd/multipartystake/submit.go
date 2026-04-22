// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func submitTxCmd() *cobra.Command {
	var (
		txFilePath  string
		sigFilesStr string
	)

	cmd := &cobra.Command{
		Use:   "submit-tx",
		Short: "Assemble signatures and submit the AddPermissionlessValidatorTx",
		RunE: func(_ *cobra.Command, _ []string) error {
			sigFiles := strings.Split(sigFilesStr, ",")
			return runSubmitTx(txFilePath, sigFiles)
		},
	}
	cmd.Flags().StringVar(&txFilePath, "tx-file", "", "Path to the prepare output JSON")
	cmd.Flags().StringVar(&sigFilesStr, "sig-files", "", "Comma-separated paths to partial signature files")
	_ = cmd.MarkFlagRequired("tx-file")
	_ = cmd.MarkFlagRequired("sig-files")
	return cmd
}

func runSubmitTx(txFilePath string, sigFiles []string) error {
	// Load prepare output
	prepareBytes, err := os.ReadFile(txFilePath)
	if err != nil {
		return fmt.Errorf("reading tx file: %w", err)
	}

	var prepareOutput PrepareOutput
	if err := json.Unmarshal(prepareBytes, &prepareOutput); err != nil {
		return fmt.Errorf("parsing tx file: %w", err)
	}

	// Deserialize unsigned tx
	unsignedBytes, err := hex.DecodeString(prepareOutput.UnsignedTxHex)
	if err != nil {
		return fmt.Errorf("decoding unsigned tx hex: %w", err)
	}

	var utx txs.UnsignedTx
	if _, err := txs.Codec.Unmarshal(unsignedBytes, &utx); err != nil {
		return fmt.Errorf("unmarshaling unsigned tx: %w", err)
	}

	addPermValTx, ok := utx.(*txs.AddPermissionlessValidatorTx)
	if !ok {
		return fmt.Errorf("expected AddPermissionlessValidatorTx, got %T", utx)
	}

	// Initialize empty credentials for all inputs
	creds := make([]verify.Verifiable, len(addPermValTx.Ins))
	for i, input := range addPermValTx.Ins {
		sigCount := numSigIndicesFromInput(input)
		creds[i] = &secp256k1fx.Credential{
			Sigs: make([][secp256k1.SignatureLen]byte, sigCount),
		}
	}

	// Load and apply each partial signature file
	for _, sigFile := range sigFiles {
		sigFileBytes, err := os.ReadFile(strings.TrimSpace(sigFile))
		if err != nil {
			return fmt.Errorf("reading sig file %s: %w", sigFile, err)
		}

		var partialSig PartialSignature
		if err := json.Unmarshal(sigFileBytes, &partialSig); err != nil {
			return fmt.Errorf("parsing sig file %s: %w", sigFile, err)
		}

		if len(partialSig.Credentials) != len(creds) {
			return fmt.Errorf("sig file %s has %d credentials, expected %d", sigFile, len(partialSig.Credentials), len(creds))
		}

		for i, credData := range partialSig.Credentials {
			cred := creds[i].(*secp256k1fx.Credential)
			for j, sigHex := range credData.Signatures {
				if j >= len(cred.Sigs) {
					return fmt.Errorf("too many signatures for input %d", i)
				}
				sigBytes, err := hex.DecodeString(sigHex)
				if err != nil {
					return fmt.Errorf("decoding signature: %w", err)
				}
				// Only overwrite if the slot is empty (zero)
				var empty [secp256k1.SignatureLen]byte
				if cred.Sigs[j] == empty {
					copy(cred.Sigs[j][:], sigBytes)
				}
			}
		}
	}

	// Build the signed transaction
	tx := &txs.Tx{
		Unsigned: utx,
		Creds:    creds,
	}

	signedBytes, err := txs.Codec.Marshal(txs.CodecVersion, tx)
	if err != nil {
		return fmt.Errorf("marshaling signed tx: %w", err)
	}
	tx.SetBytes(unsignedBytes, signedBytes)

	// Submit to network
	ctx := context.Background()
	client := platformvm.NewClient(uri)
	txID, err := client.IssueTx(ctx, signedBytes)
	if err != nil {
		return fmt.Errorf("issuing tx: %w", err)
	}

	fmt.Printf("Submitted tx %s\n", txID)
	return nil
}
