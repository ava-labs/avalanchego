// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"

	"github.com/google/uuid"
)

// Handler holds the HTTP handler state for the multi-party staking API.
type Handler struct {
	store *PartyStore
	uri   string
}

// NewHandler creates a new Handler connected to the given Avalanche node URI.
func NewHandler(uri string) *Handler {
	return &Handler{
		store: NewPartyStore(),
		uri:   uri,
	}
}

// RegisterRoutes attaches all API routes to the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/parties", h.handleParties)
	mux.HandleFunc("/api/parties/", h.handlePartyRoutes)
	mux.HandleFunc("/api/dev/derive-address", h.deriveAddress)
}

func (h *Handler) handleParties(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createParty(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handlePartyRoutes(w http.ResponseWriter, r *http.Request) {
	// Parse: /api/parties/{partyID}[/sign-validator|/sign-split]
	path := strings.TrimPrefix(r.URL.Path, "/api/parties/")
	parts := strings.SplitN(path, "/", 2)
	partyID := parts[0]

	if partyID == "" {
		writeError(w, http.StatusBadRequest, "missing party ID")
		return
	}

	if len(parts) == 1 || parts[1] == "" {
		// /api/parties/{partyID}
		switch r.Method {
		case http.MethodGet:
			h.getParty(w, partyID)
		case http.MethodOptions:
			w.WriteHeader(http.StatusNoContent)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	subPath := parts[1]
	switch {
	case subPath == "sign-validator":
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.signValidator(w, r, partyID)
	case subPath == "sign-split":
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.signSplit(w, r, partyID)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) createParty(w http.ResponseWriter, r *http.Request) {
	var config InputConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	prepareOutput, err := prepare(r.Context(), h.uri, config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("prepare failed: %v", err))
		return
	}

	partyID := uuid.New().String()
	party := &Party{
		ID:            partyID,
		Config:        config,
		State:         StateAwaitingValidatorSigs,
		PrepareOutput: prepareOutput,
		ValidatorSigs: make(map[string]*PartialSignature),
		SplitSigs:     make(map[string]*PartialSignature),
	}
	h.store.Put(party)

	needed := signaturesNeeded(config)
	writeJSON(w, http.StatusCreated, createPartyResponse{
		PartyID:            partyID,
		PrepareOutput:      prepareOutput,
		State:              party.State,
		SignaturesNeeded:   needed,
		SignaturesReceived: []string{},
	})
}

func (h *Handler) getParty(w http.ResponseWriter, partyID string) {
	party, err := h.store.Get(partyID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	needed := signaturesNeeded(party.Config)

	var splitSigsReceived []string
	for addr := range party.SplitSigs {
		splitSigsReceived = append(splitSigsReceived, addr)
	}
	if splitSigsReceived == nil {
		splitSigsReceived = []string{}
	}

	var valSigsReceived []string
	for addr := range party.ValidatorSigs {
		valSigsReceived = append(valSigsReceived, addr)
	}
	if valSigsReceived == nil {
		valSigsReceived = []string{}
	}

	writeJSON(w, http.StatusOK, getPartyResponse{
		PartyID:                 partyID,
		State:                   party.State,
		PrepareOutput:           party.PrepareOutput,
		SignaturesNeeded:        needed,
		SignaturesReceived:      valSigsReceived,
		ValidatorTxID:          party.ValidatorTxID,
		SplitOutput:            party.SplitOutput,
		SplitSignaturesReceived: splitSigsReceived,
		SplitTxID:              party.SplitTxID,
		PotentialReward:        party.PotentialReward,
	})
}

func (h *Handler) signValidator(w http.ResponseWriter, r *http.Request, partyID string) {
	var req signRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	// We need the party state to resolve dev-sign (needs unsignedTxHex + utxos)
	// so peek at it first for the privateKeyCB58 path.
	var partialSig PartialSignature
	if req.PrivateKeyCB58 != "" {
		// Dev path: server signs internally with the provided private key
		p, err := h.store.Get(partyID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		ps, err := signWithPrivateKey(req.PrivateKeyCB58, p.PrepareOutput.UnsignedTxHex, p.PrepareOutput.UTXOs)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("dev signing failed: %v", err))
			return
		}
		partialSig = *ps
	} else if req.SignedTxHex != "" {
		// Web path: extract credentials from Core Wallet's signed tx
		ps, err := extractPartialSigFromSignedTx(req.SignedTxHex, req.Address)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("extracting credentials from signed tx: %v", err))
			return
		}
		partialSig = *ps
	} else {
		// CLI path: pre-extracted credentials
		partialSig = PartialSignature{
			Address:     req.Address,
			Credentials: req.Credentials,
		}
	}

	// We need to do the state mutation and potentially trigger the
	// submit+prepare-split workflow. Use WithLock for the state check and
	// signature storage, then release the lock before doing network I/O.
	var (
		shouldSubmit bool
		party        *Party
	)

	err := h.store.WithLock(partyID, func(p *Party) error {
		if p.State != StateAwaitingValidatorSigs {
			return fmt.Errorf("party is in state %q, expected %q", p.State, StateAwaitingValidatorSigs)
		}

		// Validate the address is one of the expected stakers
		if !isExpectedSigner(p.Config, partialSig.Address) {
			return fmt.Errorf("address %s is not an expected signer", partialSig.Address)
		}

		// Reject duplicate submissions
		if _, exists := p.ValidatorSigs[partialSig.Address]; exists {
			return fmt.Errorf("address %s has already submitted a signature", partialSig.Address)
		}

		p.ValidatorSigs[partialSig.Address] = &partialSig

		if len(p.ValidatorSigs) == len(p.Config.Stakers) {
			shouldSubmit = true
		}
		party = p
		return nil
	})
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	if shouldSubmit {
		if err := h.submitValidatorAndPrepareSplit(r.Context(), party); err != nil {
			log.Printf("ERROR: submit validator tx for party %s: %v", partyID, err)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("submit failed: %v", err))
			return
		}
	}

	// Read current state for the response
	party, _ = h.store.Get(partyID)
	var received []string
	for addr := range party.ValidatorSigs {
		received = append(received, addr)
	}

	writeJSON(w, http.StatusOK, signValidatorResponse{
		Accepted:           true,
		State:              party.State,
		SignaturesReceived: received,
		ValidatorTxID:      party.ValidatorTxID,
		PotentialReward:    party.PotentialReward,
		SplitOutput:        party.SplitOutput,
	})
}

func (h *Handler) submitValidatorAndPrepareSplit(ctx context.Context, party *Party) error {
	// Issue the validator tx
	txID, err := assembleAndSubmitValidatorTx(ctx, h.uri, party.PrepareOutput, party.ValidatorSigs)
	if err != nil {
		return fmt.Errorf("assembling/submitting validator tx: %w", err)
	}

	log.Printf("Validator tx issued: %s", txID)

	// Parse the NodeID to query getCurrentValidators
	nodeID, err := ids.NodeIDFromString(party.Config.Validator.NodeID)
	if err != nil {
		return fmt.Errorf("parsing node ID: %w", err)
	}

	potentialReward, err := fetchPotentialReward(ctx, h.uri, nodeID)
	if err != nil {
		return fmt.Errorf("fetching potential reward: %w", err)
	}

	log.Printf("Potential reward for %s: %d", nodeID, potentialReward)

	// Prepare the split tx
	splitOutput, err := prepareSplit(party.PrepareOutput, party.ValidatorSigs, potentialReward)
	if err != nil {
		return fmt.Errorf("preparing split tx: %w", err)
	}

	// Update state atomically
	return h.store.WithLock(party.ID, func(p *Party) error {
		p.ValidatorTxID = txID.String()
		p.PotentialReward = potentialReward
		p.SplitOutput = splitOutput
		p.State = StateAwaitingSplitSigs
		return nil
	})
}

func (h *Handler) signSplit(w http.ResponseWriter, r *http.Request, partyID string) {
	var req signRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	var partialSig PartialSignature
	if req.PrivateKeyCB58 != "" {
		p, err := h.store.Get(partyID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if p.SplitOutput == nil {
			writeError(w, http.StatusBadRequest, "split output not available yet")
			return
		}
		ps, err := signWithPrivateKey(req.PrivateKeyCB58, p.SplitOutput.UnsignedTxHex, p.SplitOutput.UTXOs)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("dev signing failed: %v", err))
			return
		}
		partialSig = *ps
	} else if req.SignedTxHex != "" {
		ps, err := extractPartialSigFromSignedTx(req.SignedTxHex, req.Address)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("extracting credentials from signed tx: %v", err))
			return
		}
		partialSig = *ps
	} else {
		partialSig = PartialSignature{
			Address:     req.Address,
			Credentials: req.Credentials,
		}
	}

	var (
		shouldSubmit bool
		party        *Party
	)

	err := h.store.WithLock(partyID, func(p *Party) error {
		if p.State != StateAwaitingSplitSigs {
			return fmt.Errorf("party is in state %q, expected %q", p.State, StateAwaitingSplitSigs)
		}

		if !isExpectedSplitSigner(p.SplitOutput, partialSig.Address) {
			return fmt.Errorf("address %s is not an expected split signer", partialSig.Address)
		}

		// Reject duplicate submissions
		if _, exists := p.SplitSigs[partialSig.Address]; exists {
			return fmt.Errorf("address %s has already submitted a split signature", partialSig.Address)
		}

		p.SplitSigs[partialSig.Address] = &partialSig

		if len(p.SplitSigs) == len(p.SplitOutput.Signers) {
			shouldSubmit = true
		}
		party = p
		return nil
	})
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	if shouldSubmit {
		splitTxID, err := assembleAndSubmitSplitTx(r.Context(), h.uri, party.SplitOutput, party.SplitSigs)
		if err != nil {
			log.Printf("ERROR: submit split tx for party %s: %v", partyID, err)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("submit split failed: %v", err))
			return
		}

		log.Printf("Split tx issued: %s", splitTxID)

		_ = h.store.WithLock(partyID, func(p *Party) error {
			p.SplitTxID = splitTxID.String()
			p.State = StateComplete
			return nil
		})
	}

	party, _ = h.store.Get(partyID)
	var received []string
	for addr := range party.SplitSigs {
		received = append(received, addr)
	}

	writeJSON(w, http.StatusOK, signSplitResponse{
		Accepted:                true,
		State:                   party.State,
		SplitSignaturesReceived: received,
		SplitTxID:               party.SplitTxID,
	})
}

// deriveAddress derives a P-chain address from a CB58-encoded private key.
// Dev/testing only.
func (h *Handler) deriveAddress(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		PrivateKeyCB58 string `json:"privateKeyCB58"`
		NetworkID      uint32 `json:"networkID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	var privKey secp256k1.PrivateKey
	if err := privKey.UnmarshalText([]byte(req.PrivateKeyCB58)); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid private key: %v", err))
		return
	}

	networkID := req.NetworkID
	if networkID == 0 {
		networkID = constants.FujiID
	}
	hrp := constants.GetHRP(networkID)
	addr := privKey.Address()
	addrStr, err := address.Format("P", hrp, addr[:])
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("formatting address: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"address": addrStr})
}

// signaturesNeeded returns the list of staker addresses for the party config.
func signaturesNeeded(config InputConfig) []string {
	needed := make([]string, len(config.Stakers))
	for i, s := range config.Stakers {
		needed[i] = s.Address
	}
	return needed
}

// isExpectedSigner checks whether the given address is one of the expected
// stakers in the config.
func isExpectedSigner(config InputConfig, addr string) bool {
	for _, s := range config.Stakers {
		if s.Address == addr {
			return true
		}
	}
	return false
}

// isExpectedSplitSigner checks whether the given address is one of the
// expected signers for the split tx.
func isExpectedSplitSigner(splitOutput *SplitPrepareOutput, addr string) bool {
	if splitOutput == nil {
		return false
	}
	for _, s := range splitOutput.Signers {
		if s.Address == addr {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("ERROR: encoding JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
