// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	psigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	pchain "github.com/ava-labs/avalanchego/wallet/chain/p"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

type stakerData struct {
	addr    ids.ShortID
	addrSet set.Set[ids.ShortID]
	amount  uint64
}

// prepare builds an unsigned AddPermissionlessValidatorTx from the given
// configuration. This mirrors the logic in cmd/multipartystake/prepare.go.
func prepare(ctx context.Context, uri string, config InputConfig) (*PrepareOutput, error) {
	if len(config.Stakers) == 0 {
		return nil, fmt.Errorf("at least one staker is required")
	}

	feePayerIdx := -1
	for i, s := range config.Stakers {
		if s.FeePayer {
			if feePayerIdx != -1 {
				return nil, fmt.Errorf("multiple fee payers specified (indices %d and %d)", feePayerIdx, i)
			}
			feePayerIdx = i
		}
	}
	if feePayerIdx == -1 {
		return nil, fmt.Errorf("no fee payer specified: exactly one staker must have \"feePayer\": true")
	}

	nodeID, err := ids.NodeIDFromString(config.Validator.NodeID)
	if err != nil {
		return nil, fmt.Errorf("parsing node ID: %w", err)
	}

	blsPKBytes, err := hex.DecodeString(stripHexPrefix(config.Validator.BLSPublicKey))
	if err != nil {
		return nil, fmt.Errorf("parsing BLS public key: %w", err)
	}
	blsSigBytes, err := hex.DecodeString(stripHexPrefix(config.Validator.BLSProofOfPossession))
	if err != nil {
		return nil, fmt.Errorf("parsing BLS proof of possession: %w", err)
	}

	pop := &signer.ProofOfPossession{}
	if len(blsPKBytes) != bls.PublicKeyLen {
		return nil, fmt.Errorf("BLS public key must be %d bytes, got %d", bls.PublicKeyLen, len(blsPKBytes))
	}
	copy(pop.PublicKey[:], blsPKBytes)
	if len(blsSigBytes) != bls.SignatureLen {
		return nil, fmt.Errorf("BLS signature must be %d bytes, got %d", bls.SignatureLen, len(blsSigBytes))
	}
	copy(pop.ProofOfPossession[:], blsSigBytes)

	var totalWeight uint64
	for _, s := range config.Stakers {
		totalWeight += s.Amount
	}

	stakers := make([]stakerData, len(config.Stakers))
	for i, s := range config.Stakers {
		addr, err := address.ParseToID(s.Address)
		if err != nil {
			return nil, fmt.Errorf("parsing staker address %q: %w", s.Address, err)
		}
		stakers[i] = stakerData{
			addr:    addr,
			addrSet: set.Of(addr),
			amount:  s.Amount,
		}
	}

	allAddrs := set.Set[ids.ShortID]{}
	for _, s := range stakers {
		allAddrs.Add(s.addr)
	}

	_, pCTX, utxos, err := primary.FetchPState(ctx, uri, allAddrs)
	if err != nil {
		return nil, fmt.Errorf("fetching P-chain state: %w", err)
	}

	vdr := &txs.SubnetValidator{
		Validator: txs.Validator{
			NodeID: nodeID,
			End:    config.Validator.EndTime,
			Wght:   totalWeight,
		},
	}

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

	baseCx, err := builder.EstimateAddPermissionlessValidatorTxComplexity(
		pop,
		validationRewardsOwner,
		delegationRewardsOwner,
	)
	if err != nil {
		return nil, fmt.Errorf("estimating complexity: %w", err)
	}

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
			gas.Dimensions{},
			nil,
			opts,
		)
		if err != nil {
			return nil, fmt.Errorf("selecting UTXOs for staker %d (%s): %w", i, config.Stakers[i].Address, err)
		}
		nonFeePayerResults = append(nonFeePayerResults, results[i])
	}

	otherCx, err := builder.ContributionComplexity(nonFeePayerResults...)
	if err != nil {
		return nil, fmt.Errorf("calculating contribution complexity: %w", err)
	}
	fullCx, err := baseCx.Add(&otherCx)
	if err != nil {
		return nil, fmt.Errorf("adding complexities: %w", err)
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
		return nil, fmt.Errorf("selecting UTXOs for fee payer (%s): %w", config.Stakers[feePayerIdx].Address, err)
	}

	utx, err := builder.NewAddPermissionlessValidatorTxFromParts(
		pCTX,
		vdr,
		pop,
		validationRewardsOwner,
		delegationRewardsOwner,
		config.Validator.DelegationFee,
		results,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("assembling transaction: %w", err)
	}

	var unsigned txs.UnsignedTx = utx
	unsignedBytes, err := txs.Codec.Marshal(txs.CodecVersion, &unsigned)
	if err != nil {
		return nil, fmt.Errorf("marshaling unsigned tx: %w", err)
	}

	unsignedTxJSON, err := json.Marshal(utx)
	if err != nil {
		return nil, fmt.Errorf("marshaling unsigned tx to JSON: %w", err)
	}

	allInputs := utx.Ins
	utxoList := make([]*avax.UTXO, 0, len(allInputs))
	for _, input := range allInputs {
		utxoID := input.InputID()
		utxo, err := pBackend.GetUTXO(ctx, constants.PlatformChainID, utxoID)
		if err != nil {
			return nil, fmt.Errorf("getting UTXO %s: %w", utxoID, err)
		}
		utxoList = append(utxoList, utxo)
	}

	utxoHexes := make([]string, len(utxoList))
	for i, utxo := range utxoList {
		utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
		if err != nil {
			return nil, fmt.Errorf("marshaling UTXO %d: %w", i, err)
		}
		utxoHexes[i] = hex.EncodeToString(utxoBytes)
	}

	signerInfos := make([]SignerInfo, len(config.Stakers))
	for i, s := range config.Stakers {
		signerInfos[i] = SignerInfo{Address: s.Address}
	}

	output := &PrepareOutput{
		UnsignedTx:    unsignedTxJSON,
		UnsignedTxHex: hex.EncodeToString(unsignedBytes),
		UTXOs:         utxoHexes,
		Signers:       signerInfos,
	}

	return output, nil
}

// assembleAndSubmitValidatorTx merges partial signatures into a fully signed
// AddPermissionlessValidatorTx and issues it to the network. This mirrors the
// logic in cmd/multipartystake/submit.go.
func assembleAndSubmitValidatorTx(
	ctx context.Context,
	uri string,
	prepareOutput *PrepareOutput,
	sigs map[string]*PartialSignature,
) (ids.ID, error) {
	unsignedBytes, err := hex.DecodeString(prepareOutput.UnsignedTxHex)
	if err != nil {
		return ids.Empty, fmt.Errorf("decoding unsigned tx hex: %w", err)
	}

	var utx txs.UnsignedTx
	if _, err := txs.Codec.Unmarshal(unsignedBytes, &utx); err != nil {
		return ids.Empty, fmt.Errorf("unmarshaling unsigned tx: %w", err)
	}

	addPermValTx, ok := utx.(*txs.AddPermissionlessValidatorTx)
	if !ok {
		return ids.Empty, fmt.Errorf("expected AddPermissionlessValidatorTx, got %T", utx)
	}

	creds := make([]verify.Verifiable, len(addPermValTx.Ins))
	for i, input := range addPermValTx.Ins {
		sigCount := numSigIndicesFromInput(input)
		creds[i] = &secp256k1fx.Credential{
			Sigs: make([][secp256k1.SignatureLen]byte, sigCount),
		}
	}

	for _, partialSig := range sigs {
		if len(partialSig.Credentials) != len(creds) {
			return ids.Empty, fmt.Errorf(
				"partial sig from %s has %d credentials, expected %d",
				partialSig.Address, len(partialSig.Credentials), len(creds),
			)
		}

		for i, credData := range partialSig.Credentials {
			cred := creds[i].(*secp256k1fx.Credential)
			for j, sigHex := range credData.Signatures {
				if j >= len(cred.Sigs) {
					return ids.Empty, fmt.Errorf("too many signatures for input %d", i)
				}
				sigBytes, err := hex.DecodeString(sigHex)
				if err != nil {
					return ids.Empty, fmt.Errorf("decoding signature: %w", err)
				}
				var empty [secp256k1.SignatureLen]byte
				if cred.Sigs[j] == empty {
					copy(cred.Sigs[j][:], sigBytes)
				}
			}
		}
	}

	tx := &txs.Tx{
		Unsigned: utx,
		Creds:    creds,
	}

	signedBytes, err := txs.Codec.Marshal(txs.CodecVersion, tx)
	if err != nil {
		return ids.Empty, fmt.Errorf("marshaling signed tx: %w", err)
	}
	tx.SetBytes(unsignedBytes, signedBytes)

	client := platformvm.NewClient(uri)
	txID, err := client.IssueTx(ctx, signedBytes)
	if err != nil {
		return ids.Empty, fmt.Errorf("issuing tx: %w", err)
	}

	return txID, nil
}

// fetchPotentialReward polls getCurrentValidators until the validator appears
// and returns its potentialReward.
func fetchPotentialReward(ctx context.Context, uri string, nodeID ids.NodeID) (uint64, error) {
	client := platformvm.NewClient(uri)

	// The validator may take a moment to appear; retry a few times.
	for attempt := 0; attempt < 10; attempt++ {
		validators, err := client.GetCurrentValidators(ctx, ids.Empty, []ids.NodeID{nodeID})
		if err != nil {
			return 0, fmt.Errorf("fetching current validators: %w", err)
		}

		for _, v := range validators {
			if v.NodeID == nodeID && v.PotentialReward != nil {
				return *v.PotentialReward, nil
			}
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	return 0, fmt.Errorf("validator %s not found in current validators after polling", nodeID)
}

// prepareSplit builds the unsigned BaseTx that splits the validation reward
// proportionally. This mirrors the logic in cmd/multipartystake/prepare_split.go.
func prepareSplit(
	ctx context.Context,
	uri string,
	prepareOutput *PrepareOutput,
	sigs map[string]*PartialSignature,
	rewardAmount uint64,
) (*SplitPrepareOutput, error) {
	unsignedBytes, err := hex.DecodeString(prepareOutput.UnsignedTxHex)
	if err != nil {
		return nil, fmt.Errorf("decoding unsigned tx hex: %w", err)
	}

	var utx txs.UnsignedTx
	if _, err := txs.Codec.Unmarshal(unsignedBytes, &utx); err != nil {
		return nil, fmt.Errorf("unmarshaling unsigned tx: %w", err)
	}

	addPermValTx, ok := utx.(*txs.AddPermissionlessValidatorTx)
	if !ok {
		return nil, fmt.Errorf("expected AddPermissionlessValidatorTx, got %T", utx)
	}

	// Rebuild the fully signed tx to compute TxID
	creds := make([]verify.Verifiable, len(addPermValTx.Ins))
	for i, input := range addPermValTx.Ins {
		sigCount := numSigIndicesFromInput(input)
		creds[i] = &secp256k1fx.Credential{
			Sigs: make([][secp256k1.SignatureLen]byte, sigCount),
		}
	}

	for _, partialSig := range sigs {
		for i, credData := range partialSig.Credentials {
			cred := creds[i].(*secp256k1fx.Credential)
			for j, sigHex := range credData.Signatures {
				if j >= len(cred.Sigs) {
					continue
				}
				sigBytes, err := hex.DecodeString(sigHex)
				if err != nil {
					return nil, fmt.Errorf("decoding signature: %w", err)
				}
				var empty [secp256k1.SignatureLen]byte
				if cred.Sigs[j] == empty {
					copy(cred.Sigs[j][:], sigBytes)
				}
			}
		}
	}

	signedTx := &txs.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	signedBytes, err := txs.Codec.Marshal(txs.CodecVersion, signedTx)
	if err != nil {
		return nil, fmt.Errorf("marshaling signed tx: %w", err)
	}
	signedTx.SetBytes(unsignedBytes, signedBytes)

	validatorTxID := signedTx.TxID

	rewardOutputIndex := uint32(len(addPermValTx.Outs) + len(addPermValTx.StakeOuts))

	rewardsOwner, ok := addPermValTx.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, fmt.Errorf("expected secp256k1fx.OutputOwners for rewards owner, got %T", addPermValTx.ValidatorRewardsOwner)
	}

	numStakers := len(rewardsOwner.Addrs)
	if numStakers == 0 {
		return nil, fmt.Errorf("no staker addresses in rewards owner")
	}

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
		return nil, fmt.Errorf("total stake is zero")
	}

	sigIndices := make([]uint32, numStakers)
	for i := range sigIndices {
		sigIndices[i] = uint32(i)
	}

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

	// Fetch dynamic-fee context to compute the split tx fee.
	pCTX, err := pchain.NewContextFromURI(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("fetching P-chain context: %w", err)
	}

	// Build a two-pass output list: first pass uses full rewardAmount to get
	// the tx's fee complexity; second pass redistributes (rewardAmount - fee).
	buildOutputs := func(distributable uint64) []*avax.TransferableOutput {
		outs := make([]*avax.TransferableOutput, 0, numStakers)
		var distributed uint64
		for i, addr := range rewardsOwner.Addrs {
			var amount uint64
			if i == numStakers-1 {
				amount = distributable - distributed
			} else {
				amount = distributable * stakerAmounts[addr] / totalStake
			}
			distributed += amount
			if amount == 0 {
				continue
			}
			outs = append(outs, &avax.TransferableOutput{
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
		avax.SortTransferableOutputs(outs, txs.Codec)
		return outs
	}

	inputs := []*avax.TransferableInput{rewardInput}
	utils.Sort(inputs)

	// First pass: initial tx to compute fee complexity.
	feeTx := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    addPermValTx.NetworkID,
		BlockchainID: constants.PlatformChainID,
		Ins:          inputs,
		Outs:         buildOutputs(rewardAmount),
	}}
	var feeUnsigned txs.UnsignedTx = feeTx
	complexity, err := fee.TxComplexity(feeUnsigned)
	if err != nil {
		return nil, fmt.Errorf("computing split tx complexity: %w", err)
	}
	gasAmt, err := complexity.ToGas(pCTX.ComplexityWeights)
	if err != nil {
		return nil, fmt.Errorf("converting complexity to gas: %w", err)
	}
	txFee, err := gasAmt.Cost(pCTX.GasPrice)
	if err != nil {
		return nil, fmt.Errorf("computing fee cost: %w", err)
	}
	if rewardAmount <= txFee {
		return nil, fmt.Errorf("reward %d too small to cover tx fee %d", rewardAmount, txFee)
	}

	// Second pass: final outputs with fee subtracted from the distributable total.
	// The input still claims the full rewardAmount; the chain takes the delta as fee.
	outputs := buildOutputs(rewardAmount - txFee)

	splitBaseTx := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    addPermValTx.NetworkID,
		BlockchainID: constants.PlatformChainID,
		Ins:          inputs,
		Outs:         outputs,
	}}

	var splitUnsigned txs.UnsignedTx = splitBaseTx
	splitUnsignedBytes, err := txs.Codec.Marshal(txs.CodecVersion, &splitUnsigned)
	if err != nil {
		return nil, fmt.Errorf("marshaling split unsigned tx: %w", err)
	}

	splitUnsignedJSON, err := json.Marshal(splitBaseTx)
	if err != nil {
		return nil, fmt.Errorf("marshaling split unsigned tx to JSON: %w", err)
	}

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
		return nil, fmt.Errorf("marshaling reward UTXO: %w", err)
	}
	utxoHexes := []string{hex.EncodeToString(rewardUTXOBytes)}

	signerInfos := make([]SignerInfo, numStakers)
	for i, addr := range rewardsOwner.Addrs {
		hrp := constants.GetHRP(addPermValTx.NetworkID)
		addrStr, err := address.Format("P", hrp, addr[:])
		if err != nil {
			return nil, fmt.Errorf("formatting address: %w", err)
		}
		signerInfos[i] = SignerInfo{Address: addrStr}
	}

	return &SplitPrepareOutput{
		UnsignedTx:    splitUnsignedJSON,
		UnsignedTxHex: hex.EncodeToString(splitUnsignedBytes),
		UTXOs:         utxoHexes,
		Signers:       signerInfos,
		ValidatorTxID: validatorTxID.String(),
	}, nil
}

// assembleAndSubmitSplitTx merges partial signatures for the split BaseTx and
// issues it to the network. This mirrors cmd/multipartystake/submit_split.go.
func assembleAndSubmitSplitTx(
	ctx context.Context,
	uri string,
	splitOutput *SplitPrepareOutput,
	sigs map[string]*PartialSignature,
) (ids.ID, error) {
	unsignedBytes, err := hex.DecodeString(splitOutput.UnsignedTxHex)
	if err != nil {
		return ids.Empty, fmt.Errorf("decoding unsigned tx hex: %w", err)
	}

	var utx txs.UnsignedTx
	if _, err := txs.Codec.Unmarshal(unsignedBytes, &utx); err != nil {
		return ids.Empty, fmt.Errorf("unmarshaling unsigned tx: %w", err)
	}

	baseTx, ok := utx.(*txs.BaseTx)
	if !ok {
		return ids.Empty, fmt.Errorf("expected BaseTx, got %T", utx)
	}

	creds := make([]verify.Verifiable, len(baseTx.Ins))
	for i, input := range baseTx.Ins {
		sigCount := numSigIndicesFromInput(input)
		creds[i] = &secp256k1fx.Credential{
			Sigs: make([][secp256k1.SignatureLen]byte, sigCount),
		}
	}

	for _, partialSig := range sigs {
		if len(partialSig.Credentials) != len(creds) {
			return ids.Empty, fmt.Errorf(
				"partial sig from %s has %d credentials, expected %d",
				partialSig.Address, len(partialSig.Credentials), len(creds),
			)
		}

		for i, credData := range partialSig.Credentials {
			cred := creds[i].(*secp256k1fx.Credential)
			for j, sigHex := range credData.Signatures {
				if j >= len(cred.Sigs) {
					return ids.Empty, fmt.Errorf("too many signatures for input %d", i)
				}
				sigBytes, err := hex.DecodeString(sigHex)
				if err != nil {
					return ids.Empty, fmt.Errorf("decoding signature: %w", err)
				}
				var empty [secp256k1.SignatureLen]byte
				if cred.Sigs[j] == empty {
					copy(cred.Sigs[j][:], sigBytes)
				}
			}
		}
	}

	tx := &txs.Tx{
		Unsigned: utx,
		Creds:    creds,
	}

	signedBytes, err := txs.Codec.Marshal(txs.CodecVersion, tx)
	if err != nil {
		return ids.Empty, fmt.Errorf("marshaling signed tx: %w", err)
	}
	tx.SetBytes(unsignedBytes, signedBytes)

	client := platformvm.NewClient(uri)
	txID, err := client.IssueTx(ctx, signedBytes)
	if err != nil {
		return ids.Empty, fmt.Errorf("issuing tx: %w", err)
	}

	return txID, nil
}

// numSigIndicesFromInput extracts the number of signature indices from a
// TransferableInput.
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

// signWithPrivateKey signs an unsigned tx using a CB58-encoded private key.
// This is for dev/testing only — the server signs internally without a wallet.
func signWithPrivateKey(privateKeyCB58 string, unsignedTxHex string, utxoHexes []string) (*PartialSignature, error) {
	var privKey secp256k1.PrivateKey
	if err := privKey.UnmarshalText([]byte(privateKeyCB58)); err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	unsignedBytes, err := hex.DecodeString(unsignedTxHex)
	if err != nil {
		return nil, fmt.Errorf("decoding unsigned tx hex: %w", err)
	}

	var utx txs.UnsignedTx
	if _, err := txs.Codec.Unmarshal(unsignedBytes, &utx); err != nil {
		return nil, fmt.Errorf("unmarshaling unsigned tx: %w", err)
	}

	tx := &txs.Tx{Unsigned: utx}
	tx.SetBytes(unsignedBytes, unsignedBytes)

	// Build UTXO backend from embedded UTXOs
	utxoStore := common.NewUTXOs()
	ctx := context.Background()
	for i, utxoHex := range utxoHexes {
		utxoBytes, err := hex.DecodeString(utxoHex)
		if err != nil {
			return nil, fmt.Errorf("decoding UTXO %d: %w", i, err)
		}
		var utxo avax.UTXO
		if _, err := txs.Codec.Unmarshal(utxoBytes, &utxo); err != nil {
			return nil, fmt.Errorf("unmarshaling UTXO %d: %w", i, err)
		}
		if err := utxoStore.AddUTXO(ctx, constants.PlatformChainID, constants.PlatformChainID, &utxo); err != nil {
			return nil, fmt.Errorf("adding UTXO: %w", err)
		}
	}

	chainUTXOs := common.NewChainUTXOs(constants.PlatformChainID, utxoStore)
	backend := &devSignerBackend{utxos: chainUTXOs}

	kc := secp256k1fx.NewKeychain(&privKey)
	txSigner := psigner.New(kc, backend)

	if err := txSigner.Sign(ctx, tx); err != nil {
		return nil, fmt.Errorf("signing tx: %w", err)
	}

	log.Printf("DEBUG signWithPrivateKey: tx.Creds has %d entries after signing", len(tx.Creds))

	// Extract credentials
	creds := make([]CredData, len(tx.Creds))
	for i, credIntf := range tx.Creds {
		cred, ok := credIntf.(*secp256k1fx.Credential)
		if !ok {
			return nil, fmt.Errorf("credential %d is %T, expected *secp256k1fx.Credential", i, credIntf)
		}
		sigs := make([]string, len(cred.Sigs))
		for j, sig := range cred.Sigs {
			sigs[j] = hex.EncodeToString(sig[:])
		}
		creds[i] = CredData{Signatures: sigs}
	}

	// Derive the P-chain address
	myAddr := privKey.Address()
	// Determine network ID from the tx to get the correct HRP
	var networkID uint32
	switch typed := utx.(type) {
	case *txs.AddPermissionlessValidatorTx:
		networkID = typed.NetworkID
	case *txs.BaseTx:
		networkID = typed.NetworkID
	default:
		networkID = constants.FujiID
	}
	hrp := constants.GetHRP(networkID)
	addrStr, err := address.Format("P", hrp, myAddr[:])
	if err != nil {
		return nil, fmt.Errorf("formatting address: %w", err)
	}

	return &PartialSignature{
		Address:     addrStr,
		Credentials: creds,
	}, nil
}

// devSignerBackend implements psigner.Backend for offline dev signing.
type devSignerBackend struct {
	utxos common.ChainUTXOs
}

func (b *devSignerBackend) GetUTXO(ctx context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error) {
	return b.utxos.GetUTXO(ctx, chainID, utxoID)
}

func (b *devSignerBackend) GetOwner(_ context.Context, _ ids.ID) (fx.Owner, error) {
	return nil, fmt.Errorf("GetOwner not supported in dev signing mode")
}

// extractPartialSigFromSignedTx parses a signed transaction hex string
// (as returned by Core Wallet's avalanche_signTransaction) and extracts
// the credentials into a PartialSignature.
func extractPartialSigFromSignedTx(signedTxHex string, addr string) (*PartialSignature, error) {
	b, err := hex.DecodeString(stripHexPrefix(signedTxHex))
	if err != nil {
		return nil, fmt.Errorf("decoding signed tx hex: %w", err)
	}

	var tx txs.Tx
	if _, err := txs.Codec.Unmarshal(b, &tx); err != nil {
		return nil, fmt.Errorf("unmarshaling signed tx: %w", err)
	}

	creds := make([]CredData, len(tx.Creds))
	for i, cred := range tx.Creds {
		secpCred, ok := cred.(*secp256k1fx.Credential)
		if !ok {
			return nil, fmt.Errorf("credential %d is %T, expected *secp256k1fx.Credential", i, cred)
		}
		sigs := make([]string, len(secpCred.Sigs))
		for j, sig := range secpCred.Sigs {
			sigs[j] = hex.EncodeToString(sig[:])
		}
		creds[i] = CredData{Signatures: sigs}
	}

	return &PartialSignature{
		Address:     addr,
		Credentials: creds,
	}, nil
}

func stripHexPrefix(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}
