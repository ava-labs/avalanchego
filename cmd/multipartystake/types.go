// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import "encoding/json"

// InputConfig is the JSON configuration provided by the coordinator to set up
// a multi-party staking transaction.
type InputConfig struct {
	Validator ValidatorConfig `json:"validator"`
	Stakers   []StakerConfig  `json:"stakers"`
}

// ValidatorConfig holds the validator parameters for the
// AddPermissionlessValidatorTx.
type ValidatorConfig struct {
	NodeID               string `json:"nodeID"`
	BLSPublicKey         string `json:"blsPublicKey"`
	BLSProofOfPossession string `json:"blsProofOfPossession"`
	EndTime              uint64 `json:"endTime"`
	DelegationFee        uint32 `json:"delegationFee"`
}

// StakerConfig describes one party's stake contribution. Exactly one staker
// must have FeePayer set to true.
type StakerConfig struct {
	Address  string `json:"address"`
	Amount   uint64 `json:"amount"`
	FeePayer bool   `json:"feePayer,omitempty"`
}

// PrepareOutput is the JSON written by the prepare step. It contains
// everything needed for offline signing and final submission.
//
// The Hex fields carry codec-serialized bytes used by sign/submit.
// The JSON fields are human-readable representations of the same data.
type PrepareOutput struct {
	UnsignedTx    json.RawMessage `json:"unsignedTx"`
	UnsignedTxHex string          `json:"unsignedTxHex"`
	UTXOs         []string        `json:"utxos"`
	Signers       []SignerInfo    `json:"signers"`
}

// SignerInfo identifies a staker participating in the multi-party tx.
type SignerInfo struct {
	Address string `json:"address"`
}

// PartialSignature is the JSON written by the sign step. It contains the
// credentials for all inputs. Inputs that this signer cannot sign will have
// empty signature slots.
type PartialSignature struct {
	Address     string     `json:"address"`
	Credentials []CredData `json:"credentials"`
}

// CredData holds the signatures for one credential (one input).
type CredData struct {
	Signatures []string `json:"signatures"`
}

// SplitPrepareOutput is the JSON written by the prepare-split step. It
// contains the unsigned BaseTx that distributes the validation reward
// proportionally to each staker.
type SplitPrepareOutput struct {
	UnsignedTx    json.RawMessage `json:"unsignedTx"`
	UnsignedTxHex string          `json:"unsignedTxHex"`
	UTXOs         []string        `json:"utxos"`
	Signers       []SignerInfo    `json:"signers"`
	ValidatorTxID string          `json:"validatorTxID"`
}
