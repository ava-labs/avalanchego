// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package modules

import (
	"bytes"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
)

type Module struct {
	// ConfigKey is the key used in json config files to specify this precompile config.
	ConfigKey string
	// Address returns the address where the stateful precompile is accessible.
	Address common.Address
	// Contract returns a thread-safe singleton that can be used as the StatefulPrecompiledContract when
	// this config is enabled.
	Contract contract.StatefulPrecompiledContract
	// Configurator is used to configure the stateful precompile when the config is enabled.
	contract.Configurator
}

type moduleArray []Module

func (m moduleArray) Len() int {
	return len(m)
}

func (m moduleArray) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

func (m moduleArray) Less(i, j int) bool {
	return bytes.Compare(m[i].Address.Bytes(), m[j].Address.Bytes()) < 0
}
