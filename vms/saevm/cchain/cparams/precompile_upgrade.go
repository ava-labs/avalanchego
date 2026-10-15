// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
)

type marshalablePrecompileUpgrades struct {
	PrecompileUpgrades []PrecompileUpgrade `json:"precompileUpgrades,omitempty"`
}

// PrecompileUpgrade is a precompile config scheduled to activate at its
// timestamp. Its JSON encoding is keyed by the precompile's config key, as
// defined in each precompile module.
type PrecompileUpgrade struct {
	precompileconfig.Config
}

// MarshalJSON marshal the precompile config into json based on the precompile key.
// Ex: {"feeManagerConfig": {...}} where "feeManagerConfig" is the key
func (u *PrecompileUpgrade) MarshalJSON() ([]byte, error) {
	res := make(map[string]precompileconfig.Config)
	res[u.Key()] = u.Config
	return json.Marshal(res)
}
