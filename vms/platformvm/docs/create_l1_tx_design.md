## Motivation

```
BEFORE                          AFTER
──────────────────────────────  ──────────────
1. CreateSubnetTx       ─┐
2. CreateChainTx        ─┼──→  CreateL1Tx
3. ConvertSubnetToL1Tx  ─┘
```

Currently, creating an Avalanche L1 required three separate P-Chain transactions: [`CreateSubnetTx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/create_subnet_tx.go), [`CreateChainTx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/create_chain_tx.go), and [`ConvertSubnetToL1Tx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/convert_subnet_to_l1_tx.go). This process is complex, requires managing temporary `SubnetAuth` credentials that become irrelevant after conversion, increases devrel support burden, and overcomplicates tooling. Additionally, Simplex benefits from this single atomic transaction since it no longer needs to manage multiple steps and intermediate credentials. 

This PR implements [`CreateL1Tx`](https://github.com/ava-labs/avalanchego/pull/5483), a simple atomic transaction that combines all three steps. It simplifies L1 creation, eliminates the intermediary subnet step, and removes the need for SubnetAuth management. This PR follows [ACP-191](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/191-seamless-l1-creation), with one deloberate deviation: 
Transaction Schema (see section below) supports one chain per subnet, not multiple chains as ACP-191 suggests. Reason for that is no one creates subnets with multiple chains. Additionally, after we convert the subnet to l1, the current flow doesn't allow the creation of any more chains on that subnet.
 


## Transaction Schema

```go
 type CreateL1Tx struct {
  // Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// A human readable name for the chain; need not be unique
	ChainName   string   `serialize:"true" json:"chainName"`

  // ID of the VM running on the chain
	VMID        ids.ID   `serialize:"true" json:"vmID"`

  // IDs of the feature extensions running on the chain
	FxIDs       []ids.ID `serialize:"true" json:"fxIDs"`

  // Byte representation of genesis state of the chain
	GenesisData []byte   `serialize:"true" json:"genesisData"`

	// Chain where the L1 validator manager lives
	ManagerChainID ids.ID              `serialize:"true" json:"chainID"`

  // Address of the L1 validator manager
	ManagerAddress types.JSONByteSlice `serialize:"true" json:"address"`

  // Initial pay-as-you-go validators for the L1
	Validators []*CreateL1Validator `serialize:"true" json:"validators"`
}
```




## Background
### [`CreateSubnetTx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/executor/standard_tx_executor.go#L741C30-L741C49):
- Takes `txs.CreateSubnetTx` as input
- Creates a new subnet on the P-Chain
- Sets the subnet owner
- The subnetID is the transaction ID
### [`CreateChainTx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/executor/standard_tx_executor.go#L193):
- Takes `txs.CreateChainTx` as input
- Requires `SubnetAuth` to prove ownership of the subnet
- Calls `state.AddChain` on execution: extracts the `subnetID` from the tx and registers the chain under `addedChains[subnetID]`
- On acceptance, calls `vm.Internal.CreateChain(chainID, tx)` to start the chain VM
- The `chainID` is the transaction ID
### [`ConvertSubnetToL1Tx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/executor/standard_tx_executor.go#L741C30-L741C49): 
- Takes `txs.ConvertSubnetToL1Tx` as input 
- Requires `SubnetAuth` to prove ownership of the subnet
- For each validator, creates an `L1Validator` with `ValidationID = subnetID.Append(validatorIndex)` and stores it in state
-  Records the conversion via `state.SetSubnetToL1Conversion(subnetID, ...)`, storing the validator manager chain and address
- After acceptance, a `SubnetToL1ConversionMessage` warp signature becomes available for the validator manager contract to bootstrap

### Current Node Startup Flow:
1.  On startup, the node calls `createSubnet` for each tracked subnet, which calls [`GetChains`](https://github.com/ava-labs/avalanchego/blob/create-l1-tx/vms/platformvm/state/state.go#L1170) to retrieve all chains associated with that subnet.
2. [`GetChains`](https://github.com/ava-labs/avalanchego/blob/create-l1-tx/vms/platformvm/state/state.go#L1170) only ever returns `*txs.CreateChainTx` transactions, so `createSubnet` only knows how to handle that type.
3. [`createSubnet`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/vm.go#L328) casts each returned transaction to `*txs.CreateChainTx` and calls `vm.Internal.CreateChain(chain.ID(), tx)`.
4. `CreateChain` extracts the chain configuration (`SubnetID`,`VMID`, `FxIDs`, `GenesisData`) into a [`chain.ChainParameters`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/config/internal.go#L100) struct, where the chain ID is the transaction ID, and queues the chain VM for startup via `QueueChainCreation`.

### Current L1 Creation Flow
1. Issue `CreateSubnetTx` → subnet is created, subnetID = txID
2. Issue [`CreateChainTx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/create_chain_tx.go) → chain is created under the subnet, chainID = txID
3. Issue `ConvertSubnetToL1Tx` → subnet is converted to an L1, initial validators are registered, and a warp message is produced for the validator manager contract to bootstrap

## How [`CreateL1Tx`](https://github.com/ava-labs/avalanchego/blob/create-l1-tx/vms/platformvm/txs/create_l1_tx.go) Works

CreateL1Tx atomically replaces all three steps above:

1. Creates a new subnet. The `SubnetID` is derived deterministically as the transaction ID, eliminating the need for `CreateSubnetTx`.

2. Creates a chain. Chain configuration (`ChainName`, `vmID`, `fxIDs`, `genesisData`) is embedded directly in the transaction. The `BlockchainID` is defined as the `SHA256` hash of the 33 bytes resulting from concatenating the 32 byte subnetID with a single `0x00` byte (`SHA256(subnetID || 0x00)`)

3. Converts the Subnet to an L1. Sets the validator manager (`managerChainID`, `managerAddress`) and registers the initial validator set with their BLS keys and balances, identical to what `ConvertSubnetToL1Tx` does.

### Key Implementation details: 
- `CreateL1Chain` (new): Since [`CreateL1Tx`](https://github.com/ava-labs/avalanchego/blob/create-l1-tx/vms/platformvm/txs/create_l1_tx.go) has no `SubnetID` field, a new `CreateL1Chain(subnetID, tx)` method mirrors [`CreateChainTx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/create_chain_tx.go) but takes `subnetID` explicitly and derives the `BlockchainID` via `tx.BlockchainID(subnetID)`. Both methods build the same `chains.ChainParameters` struct and call `QueueChainCreation`.
- [`createSubnet`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/vm.go#L328) type switch: Since [`GetChains`](https://github.com/ava-labs/avalanchego/blob/create-l1-tx/vms/platformvm/state/state.go#L1170) now returns either `*txs.CreateChainTx` or `*txs.CreateL1Tx`, a type switch was added: `CreateChainTx` calls [`CreateChain`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/config/internal.go#L93C20-L93C31), `CreateL1Tx` calls [`CreateL1Chain`](https://github.com/ava-labs/avalanchego/blob/create-l1-tx/vms/platformvm/config/internal.go#L115).
- `state.AddL1Chain` and `Diff.Apply`: `AddChain` casts the tx to `*txs.CreateChainTx` to extract the `subnetID`. Since `CreateL1Tx` has no `SubnetID` field, `AddL1Chain` takes `subnetID`as an explicit parameter instead. `Diff.Apply` was updated with a type switch to route `CreateChainTx` to `AddChain` and all other types to `AddL1Chain`.
- No `SubnetAuth` is needed since the subnet is created atomically within the same transaction
### Backwards Compatibility: 
- [`CreateL1Tx`](https://github.com/ava-labs/avalanchego/blob/create-l1-tx/vms/platformvm/txs/create_l1_tx.go) stores its own `txID` under the same `chains/{subnetID}` prefix as `CreateChainTx`. [`GetChains`](https://github.com/ava-labs/avalanchego/blob/create-l1-tx/vms/platformvm/state/state.go#L1170) now returns a mix of both types. All callers were updated with type switches to handle both, preserving full compatibility with existing subnets.
- The `validationID` for each initial validator is `subnetID.Append(validatorIndex)`, compatible with
existing validator manager contracts
- After acceptance, a `SubnetToL1ConversionMessage` warp signature is available, ensuring
compatibility with existing validator manager infrastructure



## Testing Plan

### Unit tests:
  - `TestCreateL1TxSerialization`: verifies binary codec encoding (simple + complex) similar to `TestConvertSubnetToL1TxSerialization`
  - `TestCreateL1TxSyntacticVerify`: covers all validation error paths (invalid VMID, name too long,
  bad validators, unsorted fxIDs, etc.) and success cases, similar to `TestConvertSubnetToL1TxSyntacticVerify`
  - `TestCreateL1TxBlockchainID`: verifies deterministic blockchainID derivation matches the ACP-191
  spec and differs from validationID
  - `TestStandardExecutorCreateL1Tx`: covers executor semantic checks (Etna gate, memo length,
  validator capacity, duplicate validators, insufficient fees, balance overflow) and verifies all
  state changes on success
  - `TestStateAddL1Chain`: verifies `state.AddL1Chain` stores the chain and `state.GetChains` returns both
  `CreateChainTx` and `CreateL1Tx` entries for the same subnet
  - `TestDiffAddL1Chain`: verifies `diff.AddL1Chain` + `diff.Apply` correctly routes to `state.AddL1Chain`
  - `TestCreateL1Chain`: full VM integration test: issue tx, build and accept block, verify committed
  state (`GetTx`, `GetSubnetIDs`, `GetChains`)
  - `TestCreateSubnetChainTypes`: verifies `createSubnet` handles `CreateChainTx`, `CreateL1Tx`, mixed
  chains, and returns an error for unknown types

### E2E test:
  - Added "atomically creates an L1 using `CreateL1Tx`" in tests/e2e/p/l1.go.  Runs a full local
  network, issues the transaction, and verifies the subnet conversion ID, validator set, and L1
  validator state via the P-Chain API


## Milestones: 

### Codebase Review:
First, I had to read the relevant files and methods to familiarize myself with the current transaction flow, along with ACP-191.

### Design:
After getting familiar with the codebase, I started working on the design. 


### Implementation

After having a clear design, I started implementing the new transaction functionality. The order of the files implemented:

#### Transaction definition (new)
  1. txs/create_l1_tx.go 
  2. txs/create_l1_tx_test.go 
  3. txs/create_l1_tx_test_simple.json 
  4. txs/create_l1_tx_test_complex.json 
  
 #### Visitor/codec plumbing
  5. txs/visitor.go 
  6. txs/codec.go 
  7. txs/executor/atomic_tx_executor.go 
  8. txs/executor/proposal_tx_executor.go 
  9. txs/executor/warp_verifier.go
  10. utxo/verifier.go 

 #### Fee complexity
  11. txs/fee/complexity.go 

 #### Executor
  12. txs/executor/standard_tx_executor.go 
  13. txs/executor/standard_tx_executor_test.go 

 #### State / Diff
  14. state/state.go 
  15. state/state_test.go 
  16. state/diff.go 
  17. state/diff_test.go 

 #### VM
  18. vm.go 
  19. Vm_test.go 

####  Config
  20. config/internal.go

 #### Metrics
  21. metrics/tx_metrics.go

 #### Wallet
  22. wallet/chain/p/builder/builder.go
  23. wallet/chain/p/builder/with_options.go 
  24. wallet/chain/p/signer/visitor.go 
  25. wallet/chain/p/wallet/backend_visitor.go 
  26. wallet/chain/p/wallet/wallet.go 
  27. wallet/chain/p/wallet/with_options.go 

 #### E2E test
  28. tests/e2e/p/l1.go 

 #### Bazel
  29. txs/BUILD.bazel


### Testing: 

After Finalizing the implementation, I started testing the functionality, getting inspiration from the existing unit tests and e2e test (l1.go) (see "Testing Plan" for more details on the test suite).





