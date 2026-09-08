// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

// This file temporarily accepts deprecated coreth (pre-SAE C-Chain) options so
// that existing operator configs continue to work across the SAE transition.
//
// TODO(JonathanOppenheimer): delete this file in the next release after the
// SAE transition!

import (
	"errors"
	"fmt"
	"slices"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
)

var errUnknownLegacyEthAPI = errors.New(`unknown "eth-apis" name`)

// deprecatedConfig holds the coreth options that [config] still accepts.
type deprecatedConfig struct {
	EthAPIs []string `json:"eth-apis"`
}

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
	"eth-filter":           set.Of(rpc.APISubscription),
	"internal-account":     nil, // eth_accounts no longer exists
	"internal-blockchain":  set.Of(rpc.APIChain, rpc.APIAvalanche),
	"internal-debug":       set.Of(rpc.APIDB),
	"internal-eth":         set.Of(rpc.APIPrice, rpc.APIAvalanche),
	"internal-personal":    nil, // the personal namespace no longer exists
	"internal-transaction": set.Of(rpc.APITx),
	"internal-tx-pool":     set.Of(rpc.APITxPool),
	"net":                  set.Of(rpc.APINet),
	"web3":                 set.Of(rpc.APIWeb3),
}

// applyDeprecatedAPINames sets c.APIs from the deprecated c.EthAPIs, then
// clears c.EthAPIs. It logs the equivalent "apis" value so the operator can
// migrate. If apisSet, "apis" takes precedence and it only clears c.EthAPIs.
func (c *config) applyDeprecatedAPINames(log logging.Logger, apisSet bool) error {
	names := c.EthAPIs
	c.EthAPIs = nil

	// A config that sets both options is the expected migration path: coreth
	// reads "eth-apis" before the SAE transition and this VM reads "apis"
	// after it.
	if apisSet {
		log.Warn(`ignoring deprecated "eth-apis" because "apis" is set; "eth-apis" will be removed in the next release`)
		return nil
	}

	apis := set.NewSet[rpc.API](len(names))
	var removed []string
	for _, name := range names {
		mapped, ok := legacyEthAPIs[name]
		if !ok {
			return fmt.Errorf("%w: %q", errUnknownLegacyEthAPI, name)
		}
		if mapped.Len() == 0 {
			removed = append(removed, name)
			continue
		}
		apis.Union(mapped)
	}
	c.APIs = apis

	names = make([]string, 0, apis.Len())
	for api := range apis {
		names = append(names, string(api))
	}
	slices.Sort(names)
	log.Warn(`"eth-apis" is deprecated and will be removed in the next release; set "apis" instead`,
		zap.Strings("apis", names),
	)
	if len(removed) > 0 {
		log.Warn(`ignoring "eth-apis" names whose methods no longer exist`,
			zap.Strings("names", removed),
		)
	}
	return nil
}
