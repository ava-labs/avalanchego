// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

// This file temporarily accepts deprecated coreth (pre-SAE C-Chain) options so
// that existing operator configs continue to work across the SAE transition.
//
// TODO(JonathanOppenheimer): delete this file in the next release after the
// SAE transition!

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
)

var errUnknownLegacyEthAPI = errors.New(`unknown "eth-apis" name`)

// legacyEthAPIs maps each name that coreth's `eth-apis` option accepted to the
// [rpc.API]s that serve the same methods. A nil set means that the methods no
// longer exist.
var legacyEthAPIs = map[string]set.Set[rpc.API]{
	"admin":                nil, // the admin namespace no longer exists
	"debug":                nil, // eth's debug service (e.g. debug_dumpBlock) no longer exists
	"debug-file-tracer":    set.Of(rpc.APITrace),
	"debug-handler":        set.Of(rpc.APIProfile),
	"debug-tracer":         set.Of(rpc.APITrace),
	"eth":                  nil, // eth_etherbase and eth_coinbase no longer exist
	"eth-filter":           set.Of(rpc.APISubscriptions),
	"internal-account":     nil, // eth_accounts no longer exists
	"internal-blockchain":  set.Of(rpc.APIChain, rpc.APIAvalanche),
	"internal-debug":       set.Of(rpc.APIDB),
	"internal-eth":         set.Of(rpc.APIGas, rpc.APIAvalanche),
	"internal-personal":    nil, // the personal namespace no longer exists
	"internal-transaction": set.Of(rpc.APITransactions),
	"internal-tx-pool":     set.Of(rpc.APITxPool),
	"net":                  set.Of(rpc.APINet),
	"web3":                 set.Of(rpc.APIWeb3),
}

// applyDeprecated maps the deprecated options in keys onto their [config]
// equivalents and deletes them from keys. It returns warnings to log for the
// operator.
func (c *config) applyDeprecated(keys map[string]json.RawMessage) ([]string, error) {
	rawNames, ok := keys["eth-apis"]
	if !ok {
		return nil, nil
	}
	delete(keys, "eth-apis")
	// A config that sets both options is the expected migration path: coreth
	// reads "eth-apis" before the SAE transition and this VM reads "apis"
	// after it.
	if _, ok := keys["apis"]; ok {
		return []string{`ignoring deprecated "eth-apis" because "apis" is set; "eth-apis" will be removed in the next release`}, nil
	}

	var names []string
	if err := json.Unmarshal(rawNames, &names); err != nil {
		return nil, fmt.Errorf(`json.Unmarshal(%T) "eth-apis": %w`, names, err)
	}

	apis := set.NewSet[rpc.API](len(names))
	var removed []string
	for _, name := range names {
		mapped, ok := legacyEthAPIs[name]
		if !ok {
			return nil, fmt.Errorf("%w: %q", errUnknownLegacyEthAPI, name)
		}
		if mapped.Len() == 0 {
			removed = append(removed, name)
			continue
		}
		apis.Union(mapped)
	}
	c.APIs = apis

	msg := fmt.Sprintf(`"eth-apis" is deprecated and will be removed in the next release; use "apis": %s instead`, quotedList(apis.List()))
	if len(removed) > 0 {
		msg += "; ignoring names whose methods no longer exist: " + quotedList(removed)
	}
	return []string{msg}, nil
}
