// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"

	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/libevm/common"
	ethparams "github.com/ava-labs/libevm/params"
)

// libevmInit would ideally be a regular init() function, but it MUST be run
// before any calls to [ChainConfig.Rules]. See `config.go` for its call site.
func libevmInit() any {
	extras = ethparams.RegisterExtras(ethparams.Extras[*ChainConfigExtra, RulesExtra]{
		ReuseJSONRoot: true, // Reuse the root JSON input when unmarshalling the extra payload.
		NewRules:      constructRulesExtra,
	})
	return nil
}

var extras ethparams.ExtraPayloads[*ChainConfigExtra, RulesExtra]

// constructRulesExtra acts as an adjunct to the [params.ChainConfig.Rules]
// method. Its primary purpose is to construct the extra payload for the
// [params.Rules] but it MAY also modify the [params.Rules].
func constructRulesExtra(c *ethparams.ChainConfig, r *ethparams.Rules, cEx *ChainConfigExtra, blockNum *big.Int, isMerge bool, timestamp uint64) RulesExtra {
	var rules RulesExtra
	if cEx == nil {
		return rules
	}
	rules.AvalancheRules = cEx.GetAvalancheRules(timestamp)

	// Initialize the stateful precompiles that should be enabled at [blockTimestamp].
	rules.Precompiles = make(map[common.Address]precompileconfig.Config)
	rules.Predicaters = make(map[common.Address]precompileconfig.Predicater)
	rules.AccepterPrecompiles = make(map[common.Address]precompileconfig.Accepter)
	for _, module := range modules.RegisteredModules() {
		if config := cEx.getActivePrecompileConfig(module.Address, timestamp); config != nil && !config.IsDisabled() {
			rules.Precompiles[module.Address] = config
			if predicater, ok := config.(precompileconfig.Predicater); ok {
				rules.Predicaters[module.Address] = predicater
			}
			if precompileAccepter, ok := config.(precompileconfig.Accepter); ok {
				rules.AccepterPrecompiles[module.Address] = precompileAccepter
			}
		}
	}

	return rules
}
