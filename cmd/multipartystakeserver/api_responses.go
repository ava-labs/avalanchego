// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

// createPartyResponse is returned by POST /api/parties.
type createPartyResponse struct {
	PartyID          string        `json:"partyID"`
	PrepareOutput    *PrepareOutput `json:"prepareOutput"`
	State            PartyState    `json:"state"`
	SignaturesNeeded []string      `json:"signaturesNeeded"`
	SignaturesReceived []string    `json:"signaturesReceived"`
}

// getPartyResponse is returned by GET /api/parties/{partyID}.
type getPartyResponse struct {
	PartyID                string              `json:"partyID"`
	State                  PartyState          `json:"state"`
	PrepareOutput          *PrepareOutput      `json:"prepareOutput"`
	SignaturesNeeded       []string            `json:"signaturesNeeded"`
	SignaturesReceived     []string            `json:"signaturesReceived"`
	ValidatorTxID          string              `json:"validatorTxID"`
	SplitOutput            *SplitPrepareOutput `json:"splitOutput"`
	SplitSignaturesReceived []string           `json:"splitSignaturesReceived"`
	SplitTxID              string              `json:"splitTxID"`
	PotentialReward        uint64              `json:"potentialReward"`
}

// signValidatorResponse is returned by POST /api/parties/{partyID}/sign-validator.
type signValidatorResponse struct {
	Accepted           bool                `json:"accepted"`
	State              PartyState          `json:"state"`
	SignaturesReceived []string            `json:"signaturesReceived"`
	ValidatorTxID      string              `json:"validatorTxID,omitempty"`
	PotentialReward    uint64              `json:"potentialReward,omitempty"`
	SplitOutput        *SplitPrepareOutput `json:"splitOutput,omitempty"`
}

// signSplitResponse is returned by POST /api/parties/{partyID}/sign-split.
type signSplitResponse struct {
	Accepted                bool       `json:"accepted"`
	State                   PartyState `json:"state"`
	SplitSignaturesReceived []string   `json:"splitSignaturesReceived"`
	SplitTxID               string     `json:"splitTxID,omitempty"`
}

// errorResponse is returned for all error conditions.
type errorResponse struct {
	Error string `json:"error"`
}
