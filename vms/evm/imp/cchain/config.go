// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeconfig"
)

// parseConfig parses b as a JSON-encoded [saeconfig.Config]. This should be
// preferred over [json.Unmarshal] because it correctly populates default
// values. Options that need the operator's attention are logged as warnings.
func parseConfig(snowCtx *snow.Context, b []byte) (saeconfig.Config, error) {
	c := saeconfig.Default()
	if err := saeconfig.Parse(snowCtx.Log, b, &c); err != nil {
		return saeconfig.Config{}, err
	}
	if err := c.Verify(snowCtx.NetworkID); err != nil {
		return saeconfig.Config{}, err
	}
	return c, nil
}
