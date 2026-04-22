// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func prepareSplitCmd() *cobra.Command {
	var (
		txFilePath   string
		sigFilesStr  string
		rewardAmount uint64
		outputPath   string
	)

	cmd := &cobra.Command{
		Use:   "prepare-split",
		Short: "Prepare an unsigned BaseTx that splits the validation reward proportionally",
		RunE: func(_ *cobra.Command, _ []string) error {
			sigFiles := strings.Split(sigFilesStr, ",")
			return runPrepareSplit(txFilePath, sigFiles, rewardAmount, outputPath)
		},
	}
	cmd.Flags().StringVar(&txFilePath, "tx-file", "", "Path to the prepare output JSON")
	cmd.Flags().StringVar(&sigFilesStr, "sig-files", "", "Comma-separated paths to partial signature files")
	cmd.Flags().Uint64Var(&rewardAmount, "reward-amount", 0, "Reward amount in nAVAX")
	cmd.Flags().StringVar(&outputPath, "output", "split-tx.json", "Path to output JSON")
	_ = cmd.MarkFlagRequired("tx-file")
	_ = cmd.MarkFlagRequired("sig-files")
	_ = cmd.MarkFlagRequired("reward-amount")
	return cmd
}

func runPrepareSplit(txFilePath string, sigFiles []string, rewardAmount uint64, outputPath string) error {
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

	// Initialize empty credentials and fill from signature files
	creds := make([]verify.Verifiable, len(addPermValTx.Ins))
	for i, input := range addPermValTx.Ins {
		sigCount := numSigIndicesFromInput(input)
		creds[i] = &secp256k1fx.Credential{
			Sigs: make([][secp256k1.SignatureLen]byte, sigCount),
		}
	}

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
				var empty [secp256k1.SignatureLen]byte
				if cred.Sigs[j] == empty {
					copy(cred.Sigs[j][:], sigBytes)
				}
			}
		}
	}

	// Build the fully signed tx to compute TxID
	signedTx := &txs.Tx{
		Unsigned: utx,
		Creds:    creds,
	}

	signedBytes, err := txs.Codec.Marshal(txs.CodecVersion, signedTx)
	if err != nil {
		return fmt.Errorf("marshaling signed tx: %w", err)
	}
	signedTx.SetBytes(unsignedBytes, signedBytes)

	validatorTxID := signedTx.TxID
	fmt.Printf("Validator TxID: %s\n", validatorTxID)

	// The reward UTXO index is after all regular outputs and stake outputs
	rewardOutputIndex := uint32(len(addPermValTx.Outs) + len(addPermValTx.StakeOuts))

	// Extract staker info from the reward owner (N-of-N multisig)
	rewardsOwner, ok := addPermValTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners)
	if !ok {
		return fmt.Errorf("expected secp256k1fx.OutputOwners for rewards owner, got %T", addPermValTx.ValidatorRewardsOwner)
	}

	numStakers := len(rewardsOwner.Addrs)
	if numStakers == 0 {
		return fmt.Errorf("no staker addresses in rewards owner")
	}

	// Compute each staker's stake amount from the StakeOuts
	stakerAmounts := make(map[ids.ShortID]uint64)
	for _, stakeOut := range addPermValTx.StakeOuts {
		out := stakeOut.Out
		if lockOut, ok2 := out.(*stakeable.LockOut); ok2 {
			out = lockOut.TransferableOut
		}
		secpOut, ok2 := out.(*secp256k1fx.TransferOutput)
		if !ok2 {
			continue
		}
		for _, addr := range secpOut.OutputOwners.Addrs {
			stakerAmounts[addr] += secpOut.Amt
		}
	}

	var totalStake uint64
	for _, addr := range rewardsOwner.Addrs {
		totalStake += stakerAmounts[addr]
	}

	if totalStake == 0 {
		return fmt.Errorf("total stake is zero")
	}

	// Build sig indices [0, 1, ..., N-1] for the N-of-N multisig input
	sigIndices := make([]uint32, numStakers)
	for i := range sigIndices {
		sigIndices[i] = uint32(i)
	}

	// The reward UTXO input
	rewardInput := &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID:        validatorTxID,
			OutputIndex: rewardOutputIndex,
		},
		Asset: avax.Asset{ID: addPermValTx.Ins[0].AssetID()},
		In: &secp256k1fx.TransferInput{
			Amt: rewardAmount,
			Input: secp256k1fx.Input{
				SigIndices: sigIndices,
			},
		},
	}

	// Build per-staker outputs proportional to their stake
	outputs := make([]*avax.TransferableOutput, 0, numStakers)
	var distributed uint64
	for i, addr := range rewardsOwner.Addrs {
		var amount uint64
		if i == numStakers-1 {
			// Last staker gets the remainder to avoid rounding errors
			amount = rewardAmount - distributed
		} else {
			amount = rewardAmount * stakerAmounts[addr] / totalStake
		}
		distributed += amount

		if amount == 0 {
			continue
		}

		hrp := constants.GetHRP(addPermValTx.NetworkID)
		addrStr, err := address.Format("P", hrp, addr[:])
		if err != nil {
			return fmt.Errorf("formatting address: %w", err)
		}

		fmt.Printf("Staker %s gets %d nAVAX (%.2f%%)\n", addrStr, amount, float64(stakerAmounts[addr])/float64(totalStake)*100)

		outputs = append(outputs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: addPermValTx.Ins[0].AssetID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
		})
	}

	inputs := []*avax.TransferableInput{rewardInput}
	utils.Sort(inputs)
	avax.SortTransferableOutputs(outputs, txs.Codec)

	splitBaseTx := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    addPermValTx.NetworkID,
		BlockchainID: constants.PlatformChainID,
		Ins:          inputs,
		Outs:         outputs,
	}}

	var splitUnsigned txs.UnsignedTx = splitBaseTx
	splitUnsignedBytes, err := txs.Codec.Marshal(txs.CodecVersion, &splitUnsigned)
	if err != nil {
		return fmt.Errorf("marshaling split unsigned tx: %w", err)
	}

	splitUnsignedJSON, err := json.Marshal(splitBaseTx)
	if err != nil {
		return fmt.Errorf("marshaling split unsigned tx to JSON: %w", err)
	}

	// Build a synthetic UTXO for the reward so sign-split can work offline
	rewardUTXO := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        validatorTxID,
			OutputIndex: rewardOutputIndex,
		},
		Asset: avax.Asset{ID: addPermValTx.Ins[0].AssetID()},
		Out: &secp256k1fx.TransferOutput{
			Amt:          rewardAmount,
			OutputOwners: *rewardsOwner,
		},
	}

	rewardUTXOBytes, err := txs.Codec.Marshal(txs.CodecVersion, rewardUTXO)
	if err != nil {
		return fmt.Errorf("marshaling reward UTXO: %w", err)
	}
	utxoHexes := []string{hex.EncodeToString(rewardUTXOBytes)}

	// Build signer list from rewards owner addresses
	signerInfos := make([]SignerInfo, numStakers)
	for i, addr := range rewardsOwner.Addrs {
		hrp := constants.GetHRP(addPermValTx.NetworkID)
		addrStr, err := address.Format("P", hrp, addr[:])
		if err != nil {
			return fmt.Errorf("formatting address: %w", err)
		}
		signerInfos[i] = SignerInfo{Address: addrStr}
	}

	output := SplitPrepareOutput{
		UnsignedTx:    splitUnsignedJSON,
		UnsignedTxHex: hex.EncodeToString(splitUnsignedBytes),
		UTXOs:         utxoHexes,
		Signers:       signerInfos,
		ValidatorTxID: validatorTxID.String(),
	}

	outputBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}

	if err := os.WriteFile(outputPath, outputBytes, 0o644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	fmt.Printf("Prepared split tx written to %s\n", outputPath)
	return nil
}

// numSigIndicesFromInput extracts the number of signature indices from a TransferableInput.
func numSigIndicesFromInput(input *avax.TransferableInput) int {
	in := input.In
	if lockIn, ok := in.(*stakeable.LockIn); ok {
		in = lockIn.TransferableIn
	}
	if secpIn, ok := in.(*secp256k1fx.TransferInput); ok {
		return len(secpIn.Input.SigIndices)
	}
	return 1
}
