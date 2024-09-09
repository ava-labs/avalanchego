// (c) 2019-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *StateProcessor {
	return &StateProcessor{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Process processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, parent *types.Header, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receipts    types.Receipts
		usedGas     = new(uint64)
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		allLogs     []*types.Log
		gp          = new(GasPool).AddGas(block.GasLimit())
	)

	// Configure any upgrades that should go into effect during this block.
	err := ApplyUpgrades(p.config, &parent.Time, block, statedb)
	if err != nil {
		log.Error("failed to configure precompiles processing block", "hash", block.Hash(), "number", block.NumberU64(), "timestamp", block.Time(), "err", err)
		return nil, nil, 0, err
	}

	var (
		context = NewEVMBlockContext(header, p.bc, nil)
		vmenv   = vm.NewEVM(context, vm.TxContext{}, statedb, p.config, cfg)
		signer  = types.MakeSigner(p.config, header.Number, header.Time)
	)
	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		ProcessBeaconBlockRoot(*beaconRoot, vmenv, statedb)
	}
	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		msg, err := TransactionToMessage(tx, signer, header.BaseFee)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		statedb.SetTxContext(tx.Hash(), i)
		receipt, err := applyTransaction(msg, p.config, gp, statedb, blockNumber, blockHash, tx, usedGas, vmenv)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	if err := p.engine.Finalize(p.bc, block, parent, statedb, receipts); err != nil {
		return nil, nil, 0, fmt.Errorf("engine finalization check failed: %w", err)
	}

	return receipts, allLogs, *usedGas, nil
}

func applyTransaction(msg *Message, config *params.ChainConfig, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, tx *types.Transaction, usedGas *uint64, evm *vm.EVM) (*types.Receipt, error) {
	// Create a new context to be used in the EVM environment.
	txContext := NewEVMTxContext(msg)
	evm.Reset(txContext, statedb)

	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, err
	}

	// Update the state with pending changes.
	var root []byte
	if config.IsByzantium(blockNumber) {
		statedb.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(config.IsEIP158(blockNumber)).Bytes()
	}
	*usedGas += result.UsedGas

	// Create a new receipt for the transaction, storing the intermediate root and gas used
	// by the tx.
	receipt := &types.Receipt{Type: tx.Type(), PostState: root, CumulativeGasUsed: *usedGas}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed
	} else {
		receipt.Status = types.ReceiptStatusSuccessful
	}
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas

	if tx.Type() == types.BlobTxType {
		receipt.BlobGasUsed = uint64(len(tx.BlobHashes()) * params.BlobTxBlobGasPerBlob)
		receipt.BlobGasPrice = evm.Context.BlobBaseFee
	}

	// If the transaction created a contract, store the creation address in the receipt.
	if msg.To == nil {
		receipt.ContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce())
	}

	// Set the receipt logs and create the bloom filter.
	receipt.Logs = statedb.GetLogs(tx.Hash(), blockNumber.Uint64(), blockHash)
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
	receipt.BlockHash = blockHash
	receipt.BlockNumber = blockNumber
	receipt.TransactionIndex = uint(statedb.TxIndex())
	return receipt, err
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(config *params.ChainConfig, bc ChainContext, blockContext vm.BlockContext, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config) (*types.Receipt, error) {
	msg, err := TransactionToMessage(tx, types.MakeSigner(config, header.Number, header.Time), header.BaseFee)
	if err != nil {
		return nil, err
	}
	// Create a new context to be used in the EVM environment
	txContext := NewEVMTxContext(msg)
	vmenv := vm.NewEVM(blockContext, txContext, statedb, config, cfg)
	return applyTransaction(msg, config, gp, statedb, header.Number, header.Hash(), tx, usedGas, vmenv)
}

// ProcessBeaconBlockRoot applies the EIP-4788 system call to the beacon block root
// contract. This method is exported to be used in tests.
func ProcessBeaconBlockRoot(beaconRoot common.Hash, vmenv *vm.EVM, statedb *state.StateDB) {
	// If EIP-4788 is enabled, we need to invoke the beaconroot storage contract with
	// the new root
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &params.BeaconRootsStorageAddress,
		Data:      beaconRoot[:],
	}
	vmenv.Reset(NewEVMTxContext(msg), statedb)
	statedb.AddAddressToAccessList(params.BeaconRootsStorageAddress)
	_, _, _ = vmenv.Call(vm.AccountRef(msg.From), *msg.To, msg.Data, 30_000_000, common.U2560)
	statedb.Finalise(true)
}

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
	for _, module := range modules.RegisteredModules() {
		for _, activatingConfig := range c.GetActivatingPrecompileConfigs(module.Address, parentTimestamp, blockTimestamp, c.PrecompileUpgrades) {
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
				if err := module.Configure(c, activatingConfig, statedb, blockContext); err != nil {
					return fmt.Errorf("could not configure precompile, name: %s, reason: %w", module.ConfigKey, err)
				}
			}
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
	return ApplyPrecompileActivations(c, parentTimestamp, blockContext, statedb)
}
