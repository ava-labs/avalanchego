// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

// verifyRequest mirrors the JSON body expected by the oracle HTTP sidecar
// protocol (see oracle/http_sidecar_client.go).
type verifyRequest struct {
	MessageBytes  string `json:"message_bytes"`
	Justification string `json:"justification"`
}

// verifyResponse is the JSON body returned by the /verify endpoint.
type verifyResponse struct {
	Error string `json:"error,omitempty"`
}

// Server is an HTTP server that handles oracle verification requests.
type Server struct {
	verifier *SolanaVerifier
}

func NewServer(v *SolanaVerifier) *Server {
	return &Server{verifier: v}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/verify" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "failed to decode request body: "+err.Error())
		return
	}

	msgBytes, err := hex.DecodeString(req.MessageBytes)
	if err != nil {
		writeError(w, "failed to hex-decode message_bytes: "+err.Error())
		return
	}

	justificationBytes, err := hex.DecodeString(req.Justification)
	if err != nil {
		writeError(w, "failed to hex-decode justification: "+err.Error())
		return
	}

	msg, err := oracle.ParseOracleMessage(msgBytes)
	if err != nil {
		writeError(w, "failed to parse OracleMessage: "+err.Error())
		return
	}

	if err := s.verifier.Verify(r.Context(), msg, justificationBytes); err != nil {
		writeError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(verifyResponse{})
}

func writeError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(verifyResponse{Error: msg})
}
