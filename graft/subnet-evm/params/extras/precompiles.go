// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

type Precompiles map[string]precompileconfig.Config

// UnmarshalJSON parses the JSON-encoded data into the ChainConfigPrecompiles.
// ChainConfigPrecompiles is a map of precompile module keys to their
// configuration.
func (ccp *Precompiles) UnmarshalJSON(data []byte) error {
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*ccp = make(Precompiles)
	for _, module := range modules.RegisteredModules() {
		key := module.ConfigKey
		if value, ok := raw[key]; ok {
			conf := module.MakeConfig()
			if err := json.Unmarshal(value, conf); err != nil {
				return err
			}
			(*ccp)[key] = conf
		}
	}
	return nil
}
