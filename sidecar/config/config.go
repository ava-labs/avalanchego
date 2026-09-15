// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package config defines the on-disk config file shared by the sidecar binary
// and the AvalancheGo validator. The sidecar consumes verifier bodies; the
// validator reads only the Verifiers map keys.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

var (
	ErrRead        = errors.New("failed to read sidecar config")
	ErrParse       = errors.New("failed to parse sidecar config")
	ErrNoVerifiers = errors.New("sidecar config has no verifiers")
)

type SidecarConfig struct {
	BindAddr  string                     `json:"bind_addr"`
	Verifiers map[string]json.RawMessage `json:"verifiers"`
}

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

// SourceTypes returns the Verifiers map keys. Contract with the validator:
// reshaping Verifiers requires updating graft/subnet-evm/plugin/evm/vm.go's loader.
func (c *SidecarConfig) SourceTypes() []string {
	out := make([]string, 0, len(c.Verifiers))
	for k := range c.Verifiers {
		out = append(out, k)
	}
	return out
}
