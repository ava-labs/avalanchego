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

// PrepareOutput is the result of preparing an unsigned
// AddPermissionlessValidatorTx. It contains everything needed for offline
// signing and final submission.
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

// PartialSignature is submitted by a staker after signing offline. It contains
// credentials for all inputs; inputs this signer cannot sign have empty slots.
type PartialSignature struct {
	Address     string     `json:"address"`
	Credentials []CredData `json:"credentials"`
}

// CredData holds the signatures for one credential (one input).
type CredData struct {
	Signatures []string `json:"signatures"`
}

// SplitPrepareOutput is the result of preparing the unsigned BaseTx that
// distributes the validation reward proportionally to each staker.
type SplitPrepareOutput struct {
	UnsignedTx    json.RawMessage `json:"unsignedTx"`
	UnsignedTxHex string          `json:"unsignedTxHex"`
	UTXOs         []string        `json:"utxos"`
	Signers       []SignerInfo    `json:"signers"`
	ValidatorTxID string          `json:"validatorTxID"`
}

// PartyState represents the current stage in the multi-party staking workflow.
type PartyState string

const (
	// StateAwaitingValidatorSigs means the party is waiting for all stakers
	// to submit partial signatures for the AddPermissionlessValidatorTx.
	StateAwaitingValidatorSigs PartyState = "awaiting_validator_signatures"
	// StateValidatorTxIssued means the validator tx has been assembled and
	// submitted to the network.
	StateValidatorTxIssued PartyState = "validator_tx_issued"
	// StateAwaitingSplitSigs means the party is waiting for all stakers to
	// submit partial signatures for the reward-split BaseTx.
	StateAwaitingSplitSigs PartyState = "awaiting_split_signatures"
	// StateComplete means the reward-split tx has been submitted; the
	// workflow is done.
	StateComplete PartyState = "complete"
)

// Party holds all state for one multi-party staking workflow instance.
type Party struct {
	ID     string      `json:"partyID"`
	Config InputConfig `json:"config"`
	State  PartyState  `json:"state"`

	// Phase 1: AddPermissionlessValidatorTx
	PrepareOutput *PrepareOutput                `json:"prepareOutput,omitempty"`
	ValidatorSigs map[string]*PartialSignature  `json:"validatorSigs,omitempty"`
	ValidatorTxID string                        `json:"validatorTxID,omitempty"`

	// Phase 2: Reward split BaseTx
	SplitOutput *SplitPrepareOutput           `json:"splitOutput,omitempty"`
	SplitSigs   map[string]*PartialSignature  `json:"splitSigs,omitempty"`
	SplitTxID   string                        `json:"splitTxID,omitempty"`

	PotentialReward uint64 `json:"potentialReward,omitempty"`
}
