// (c) 2025, Ava Labs, Inc.

package core

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/modules"
	"github.com/ava-labs/subnet-evm/stateupgrade"
)

// ApplyPrecompileActivations checks if any of the precompiles specified by the chain config are enabled or disabled by the block
// transition from [parentTimestamp] to the timestamp set in [blockContext]. If this is the case, it calls [Configure]
// to apply the necessary state transitions for the upgrade.
// This function is called within genesis setup to configure the starting state for precompiles enabled at genesis.
// In block processing and building, ApplyUpgrades is called instead which also applies state upgrades.
func ApplyPrecompileActivations(c *params.ChainConfig, parentTimestamp *uint64, blockContext contract.ConfigurationBlockContext, statedb *state.StateDB) error {
	blockTimestamp := blockContext.Timestamp()
	// Note: RegisteredModules returns precompiles sorted by module addresses.
	// This ensures that the order we call Configure for each precompile is consistent.
	// This ensures even if precompiles read/write state other than their own they will observe
	// an identical global state in a deterministic order when they are configured.
	extra := params.GetExtra(c)
	for _, module := range modules.RegisteredModules() {
		for _, activatingConfig := range extra.GetActivatingPrecompileConfigs(module.Address, parentTimestamp, blockTimestamp, extra.PrecompileUpgrades) {
			// If this transition activates the upgrade, configure the stateful precompile.
			// (or deconfigure it if it is being disabled.)
			if activatingConfig.IsDisabled() {
				log.Info("Disabling precompile", "name", module.ConfigKey)
				statedb.SelfDestruct(module.Address)
				// Calling Finalise here effectively commits Suicide call and wipes the contract state.
				// This enables re-configuration of the same contract state in the same block.
				// Without an immediate Finalise call after the Suicide, a reconfigured precompiled state can be wiped out
				// since Suicide will be committed after the reconfiguration.
				statedb.Finalise(true)
			} else {
				var printIntf interface{}
				marshalled, err := json.Marshal(activatingConfig)
				if err == nil {
					printIntf = string(marshalled)
				} else {
					printIntf = activatingConfig
				}

				log.Info("Activating new precompile", "name", module.ConfigKey, "config", printIntf)
				// Set the nonce of the precompile's address (as is done when a contract is created) to ensure
				// that it is marked as non-empty and will not be cleaned up when the statedb is finalized.
				statedb.SetNonce(module.Address, 1)
				// Set the code of the precompile's address to a non-zero length byte slice to ensure that the precompile
				// can be called from within Solidity contracts. Solidity adds a check before invoking a contract to ensure
				// that it does not attempt to invoke a non-existent contract.
				statedb.SetCode(module.Address, []byte{0x1})
				extstatedb := &extstate.StateDB{VmStateDB: statedb}
				if err := module.Configure(params.GetExtra(c), activatingConfig, extstatedb, blockContext); err != nil {
					return fmt.Errorf("could not configure precompile, name: %s, reason: %w", module.ConfigKey, err)
				}
			}
		}
	}
	return nil
}

// applyStateUpgrades checks if any of the state upgrades specified by the chain config are activated by the block
// transition from [parentTimestamp] to the timestamp set in [header]. If this is the case, it calls [Configure]
// to apply the necessary state transitions for the upgrade.
func applyStateUpgrades(c *params.ChainConfig, parentTimestamp *uint64, blockContext contract.ConfigurationBlockContext, statedb *state.StateDB) error {
	// Apply state upgrades
	configExtra := params.GetExtra(c)
	for _, upgrade := range configExtra.GetActivatingStateUpgrades(parentTimestamp, blockContext.Timestamp(), configExtra.StateUpgrades) {
		log.Info("Applying state upgrade", "blockNumber", blockContext.Number(), "upgrade", upgrade)
		if err := stateupgrade.Configure(&upgrade, c, statedb, blockContext); err != nil {
			return fmt.Errorf("could not configure state upgrade: %w", err)
		}
	}
	return nil
}

// ApplyUpgrades checks if any of the precompile or state upgrades specified by the chain config are activated by the block
// transition from [parentTimestamp] to the timestamp set in [header]. If this is the case, it calls [Configure]
// to apply the necessary state transitions for the upgrade.
// This function is called:
// - in block processing to update the state when processing a block.
// - in the miner to apply the state upgrades when producing a block.
func ApplyUpgrades(c *params.ChainConfig, parentTimestamp *uint64, blockContext contract.ConfigurationBlockContext, statedb *state.StateDB) error {
	if err := ApplyPrecompileActivations(c, parentTimestamp, blockContext, statedb); err != nil {
		return err
	}
	return applyStateUpgrades(c, parentTimestamp, blockContext, statedb)
}

type blockContext struct {
	number    *big.Int
	timestamp uint64
}

func NewBlockContext(number *big.Int, timestamp uint64) *blockContext {
	return &blockContext{
		number:    number,
		timestamp: timestamp,
	}
}

func (bc *blockContext) Number() *big.Int  { return bc.number }
func (bc *blockContext) Timestamp() uint64 { return bc.timestamp }
