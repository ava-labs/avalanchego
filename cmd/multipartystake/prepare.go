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
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func prepareCmd() *cobra.Command {
	var (
		configPath string
		outputPath string
	)

	cmd := &cobra.Command{
		Use:   "prepare",
		Short: "Prepare an unsigned multi-party AddPermissionlessValidatorTx",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPrepare(configPath, outputPath)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to input config JSON")
	cmd.Flags().StringVar(&outputPath, "output", "tx.json", "Path to output JSON")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

type stakerData struct {
	addr    ids.ShortID
	addrSet set.Set[ids.ShortID]
	amount  uint64
}

func runPrepare(configPath, outputPath string) error {
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var config InputConfig
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	if len(config.Stakers) == 0 {
		return fmt.Errorf("at least one staker is required")
	}

	// Find the fee payer
	feePayerIdx := -1
	for i, s := range config.Stakers {
		if s.FeePayer {
			if feePayerIdx != -1 {
				return fmt.Errorf("multiple fee payers specified (indices %d and %d)", feePayerIdx, i)
			}
			feePayerIdx = i
		}
	}
	if feePayerIdx == -1 {
		return fmt.Errorf("no fee payer specified: exactly one staker must have \"feePayer\": true")
	}

	// Parse validator info
	nodeID, err := ids.NodeIDFromString(config.Validator.NodeID)
	if err != nil {
		return fmt.Errorf("parsing node ID: %w", err)
	}

	blsPKBytes, err := hex.DecodeString(stripHexPrefix(config.Validator.BLSPublicKey))
	if err != nil {
		return fmt.Errorf("parsing BLS public key: %w", err)
	}
	blsSigBytes, err := hex.DecodeString(stripHexPrefix(config.Validator.BLSProofOfPossession))
	if err != nil {
		return fmt.Errorf("parsing BLS proof of possession: %w", err)
	}

	pop := &signer.ProofOfPossession{}
	if len(blsPKBytes) != bls.PublicKeyLen {
		return fmt.Errorf("BLS public key must be %d bytes, got %d", bls.PublicKeyLen, len(blsPKBytes))
	}
	copy(pop.PublicKey[:], blsPKBytes)
	if len(blsSigBytes) != bls.SignatureLen {
		return fmt.Errorf("BLS signature must be %d bytes, got %d", bls.SignatureLen, len(blsSigBytes))
	}
	copy(pop.ProofOfPossession[:], blsSigBytes)

	// Calculate total stake weight
	var totalWeight uint64
	for _, s := range config.Stakers {
		totalWeight += s.Amount
	}

	ctx := context.Background()

	// Parse all staker addresses
	stakers := make([]stakerData, len(config.Stakers))
	for i, s := range config.Stakers {
		addr, err := address.ParseToID(s.Address)
		if err != nil {
			return fmt.Errorf("parsing staker address %q: %w", s.Address, err)
		}
		stakers[i] = stakerData{
			addr:    addr,
			addrSet: set.Of(addr),
			amount:  s.Amount,
		}
	}

	// Fetch P-chain state for all addresses combined
	allAddrs := set.Set[ids.ShortID]{}
	for _, s := range stakers {
		allAddrs.Add(s.addr)
	}

	_, pCTX, utxos, err := primary.FetchPState(ctx, uri, allAddrs)
	if err != nil {
		return fmt.Errorf("fetching P-chain state: %w", err)
	}

	vdr := &txs.SubnetValidator{
		Validator: txs.Validator{
			NodeID: nodeID,
			End:    config.Validator.EndTime,
			Wght:   totalWeight,
		},
	}

	// Validation rewards require all stakers to sign (N-of-N multisig).
	// After unstaking, a follow-up BaseTx will split the reward UTXO
	// proportionally based on each staker's contribution.
	allStakerAddrs := make([]ids.ShortID, len(stakers))
	for i := range stakers {
		allStakerAddrs[i] = stakers[i].addr
	}
	validationRewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: uint32(len(stakers)),
		Addrs:     allStakerAddrs,
	}
	delegationRewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: uint32(len(stakers)),
		Addrs:     allStakerAddrs,
	}

	// Step 1: Estimate base complexity
	baseCx, err := builder.EstimateAddPermissionlessValidatorTxComplexity(
		pop,
		validationRewardsOwner,
		delegationRewardsOwner,
	)
	if err != nil {
		return fmt.Errorf("estimating complexity: %w", err)
	}

	// Step 2: Non-fee-payers select UTXOs first (zero complexity = no fee)
	results := make([]*builder.SpendResult, len(stakers))
	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, utxos)
	pBackend := wallet.NewBackend(pUTXOs, nil)

	var nonFeePayerResults []*builder.SpendResult
	for i := range stakers {
		if i == feePayerIdx {
			continue
		}
		opts := common.NewOptions([]common.Option{common.WithCustomAddresses(stakers[i].addrSet)})
		results[i], err = builder.Spend(
			pBackend,
			pCTX,
			stakers[i].addrSet,
			map[ids.ID]uint64{},
			map[ids.ID]uint64{pCTX.AVAXAssetID: stakers[i].amount},
			0,
			gas.Dimensions{}, // zero complexity = no fee
			nil,
			opts,
		)
		if err != nil {
			return fmt.Errorf("selecting UTXOs for staker %d (%s): %w", i, config.Stakers[i].Address, err)
		}
		nonFeePayerResults = append(nonFeePayerResults, results[i])
	}

	// Step 3: Fee payer accounts for all other parties' complexity
	otherCx, err := builder.ContributionComplexity(nonFeePayerResults...)
	if err != nil {
		return fmt.Errorf("calculating contribution complexity: %w", err)
	}
	fullCx, err := baseCx.Add(&otherCx)
	if err != nil {
		return fmt.Errorf("adding complexities: %w", err)
	}

	opts := common.NewOptions([]common.Option{common.WithCustomAddresses(stakers[feePayerIdx].addrSet)})
	results[feePayerIdx], err = builder.Spend(
		pBackend,
		pCTX,
		stakers[feePayerIdx].addrSet,
		map[ids.ID]uint64{},
		map[ids.ID]uint64{pCTX.AVAXAssetID: stakers[feePayerIdx].amount},
		0,
		fullCx,
		nil,
		opts,
	)
	if err != nil {
		return fmt.Errorf("selecting UTXOs for fee payer (%s): %w", config.Stakers[feePayerIdx].Address, err)
	}

	// Step 4: Assemble unsigned tx
	utx, err := builder.NewAddPermissionlessValidatorTxFromParts(
		pCTX,
		vdr,
		pop,
		validationRewardsOwner,
		delegationRewardsOwner,
		config.Validator.DelegationFee,
		results,
		nil, // no memo
	)
	if err != nil {
		return fmt.Errorf("assembling transaction: %w", err)
	}

	// Serialize unsigned tx to codec bytes (needed by sign/submit).
	// Marshal through the UnsignedTx interface so the codec writes the type ID
	// prefix, which is required for unmarshaling back into the interface.
	var unsigned txs.UnsignedTx = utx
	unsignedBytes, err := txs.Codec.Marshal(txs.CodecVersion, &unsigned)
	if err != nil {
		return fmt.Errorf("marshaling unsigned tx: %w", err)
	}

	// JSON representation of the unsigned tx (human-readable)
	unsignedTxJSON, err := json.Marshal(utx)
	if err != nil {
		return fmt.Errorf("marshaling unsigned tx to JSON: %w", err)
	}

	// Collect all UTXOs referenced by inputs for offline signing
	allInputs := utx.Ins
	utxoList := make([]*avax.UTXO, 0, len(allInputs))
	for _, input := range allInputs {
		utxoID := input.InputID()
		utxo, err := pBackend.GetUTXO(ctx, constants.PlatformChainID, utxoID)
		if err != nil {
			return fmt.Errorf("getting UTXO %s: %w", utxoID, err)
		}
		utxoList = append(utxoList, utxo)
	}

	utxoHexes := make([]string, len(utxoList))
	for i, utxo := range utxoList {
		utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("marshaling UTXO %d: %w", i, err)
		}
		utxoHexes[i] = hex.EncodeToString(utxoBytes)
	}

	// Build signer list
	signerInfos := make([]SignerInfo, len(config.Stakers))
	for i, s := range config.Stakers {
		signerInfos[i] = SignerInfo{Address: s.Address}
	}

	output := PrepareOutput{
		UnsignedTx:    unsignedTxJSON,
		UnsignedTxHex: hex.EncodeToString(unsignedBytes),
		UTXOs:         utxoHexes,
		Signers:       signerInfos,
	}

	outputBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}

	if err := os.WriteFile(outputPath, outputBytes, 0o644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	fmt.Printf("Prepared unsigned tx with %d inputs, written to %s\n", len(utx.Ins), outputPath)
	return nil
}
