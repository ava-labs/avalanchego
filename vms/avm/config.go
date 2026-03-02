// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/vms/avm/network"
)

var DefaultConfig = Config{
	Network:          network.DefaultConfig,
	ChecksumsEnabled: false,
}

type Config struct {
	Network          network.Config `json:"network"`
	ChecksumsEnabled bool           `json:"checksums-enabled"`
}

func ParseConfig(configBytes []byte) (Config, error) {
	if len(configBytes) == 0 {
		return DefaultConfig, nil
	}

	config := DefaultConfig
	err := json.Unmarshal(configBytes, &config)
	return config, err
}
