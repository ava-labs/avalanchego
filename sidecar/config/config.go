// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package config defines the on-disk configuration file shared between the
// oracle sidecar binary and the AvalancheGo validator that talks to it.
//
// The sidecar reads the file to learn which verifiers to register and how to
// configure each of them. The validator reads the same file to learn which
// source types it should accept signature requests for. Neither process's
// operation is coupled to the other's beyond this file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Errors returned by Load. Tests and callers may match with errors.Is.
var (
	ErrRead        = errors.New("failed to read sidecar config")
	ErrParse       = errors.New("failed to parse sidecar config")
	ErrNoVerifiers = errors.New("sidecar config has no verifiers")
)

// SidecarConfig is the top-level layout of the sidecar config file.
//
// The Verifiers field is the contract between the sidecar and the validator:
// the sidecar consumes the raw JSON body of each entry to construct its
// verifiers, and the validator reads the map keys (via SourceTypes) to build
// its allowed-source-type set. Reshaping this field requires updating both
// consumers; see the note on SourceTypes.
type SidecarConfig struct {
	// BindAddr is the address the sidecar's gRPC server listens on
	// (e.g. ":9900"). Only consumed by the sidecar binary.
	BindAddr string `json:"bind_addr"`

	// Verifiers maps a source-type string (e.g. "solana") to the raw JSON
	// config for the verifier that handles that source type. The sidecar
	// hands the raw bytes to the verifier's constructor, which owns the
	// schema for its own body.
	Verifiers map[string]json.RawMessage `json:"verifiers"`
}

// Load reads and parses a sidecar config file from disk.
func Load(path string) (*SidecarConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrRead, path, err)
	}
	var cfg SidecarConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrParse, path, err)
	}
	if len(cfg.Verifiers) == 0 {
		return nil, ErrNoVerifiers
	}
	return &cfg, nil
}

// SourceTypes returns the set of source types this sidecar is configured to
// verify. The AvalancheGo validator reads these keys to build its allowed
// source-type set — reshaping the Verifiers field in SidecarConfig requires
// updating the validator's loader in graft/subnet-evm/plugin/evm/vm.go.
func (c *SidecarConfig) SourceTypes() []string {
	out := make([]string, 0, len(c.Verifiers))
	for k := range c.Verifiers {
		out = append(out, k)
	}
	return out
}
