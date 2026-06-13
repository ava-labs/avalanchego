## Motivation
Currently, creating an Avalanche L1 required three separate P-Chain transactions: [`CreateSubnetTx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/create_subnet_tx.go), [`CreateChainTx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/create_chain_tx.go), and [`ConvertSubnetToL1Tx`](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/txs/convert_subnet_to_l1_tx.go).  This process is complex and requires managing temporary SubnetAuth credentials that become irrelevant after conversion. 

This PR implements [`CreateL1Tx`](https://github.com/ava-labs/avalanchego/pull/5483) as described in [ACP-191](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/191-seamless-l1-creation), a simple atomic transaction that combines all three steps. It simplifies L1 creation, eliminates the intermediary subnet step, and removes the need for SubnetAuth management.


## Transaction Schema

```go
 type CreateL1Tx struct {
	BaseTx `serialize:"true"`

	// Creating Chain Variables
	ChainName   string   `serialize:"true" json:"chainName"` // chain name
	VMID        ids.ID   `serialize:"true" json:"vmID"`.     //  ID of the VM to run on the new chain
	FxIDs       []ids.ID `serialize:"true" json:"fxIDs"`.    // Feature extension IDs
	GenesisData []byte   `serialize:"true" json:"genesisData"` // Genesis data for the chain

	// Validator Manager Variables
	ManagerChainID ids.ID              `serialize:"true" json:"chainID"` //  Chain where the validator manager contract lives
	ManagerAddress types.JSONByteSlice `serialize:"true" json:"address"` //  Address of the validator manager contract

	Validators []*ConvertSubnetToL1Validator `serialize:"true" json:"validators"` //  Initial validator set
}
```

## How this works

CreateL1Tx is a new P-Chain standard transaction that atomically:

1. Creates a new subnet. The `SubnetID` is derived deterministically as the transaction ID, eliminating the need for  the `CreateSubnetTx`.
2. Creates a chain. Chain configuration (`ChainName`, `vmID`, `fxIDs`, `genesisData`) is embedded directly in the transaction. The `BlockchainID` is derived as `SHA256(subnetID || 0x00 || chainIndex=0)`
3. Converts the Subnet to an L1. Sets the validator manager (`managerChainID`, `managerAddress`) and registers the initial validator set with their BLS keys and balances,  identical to what `ConvertSubnetToL1Tx` does.

### Key Implementation details: 
- No SubnetAuth is needed since the subnet is created atomically within the same transaction
- The validationID for each initial validator is `subnetID.Append(validatorIndex)`, compatible with existing validator manager contracts
-  After acceptance, a `SubnetToL1ConversionMessage` warp signature is available, ensuring compatibility with existing validator manager infrastructure
- The node's `createSubnet` startup path was updated to handle both `*txs.CreateChainTx` and `*txs.CreateL1Tx`: When a node starts up, it calls `createSubnet` for each tracked subnet, which calls `state.GetChains` to retrieve all chains associated with that subnet and starts the corresponding chain VMs. Previously, `GetChains` only ever returned `*txs.CreateChainTx` transactions, so `createSubnet` only knew how to handle that type. Since `CreateL1Tx` creates its chain atomically (rather than through a separate `CreateChainTx`), the chain record stored in state is a `*txs.CreateL1Tx` instead. The fix adds a type switch in `createSubnet` that calls `CreateChain` for `*txs.CreateChainTx` and the new `CreateL1Chain` method for `*txs.CreateL1Tx`. `state.AddL1Chain` and `Diff.Apply` were also updated accordingly: unlike `AddChain`, which derives the `subnetID` by casting the tx to `*txs.CreateChainTx` and reading `tx.SubnetID`, `AddL1Chain` takes `subnetID` as an explicit parameter, because `CreateL1Tx` has no `SubnetID` field; the `subnetID` is the `txID`. This way we ensure `CreateL1Tx` chains are stored and propagated through the state/diff layer correctly.
- `Diff.Apply` was similarly updated to correctly route `CreateL1Tx` chains to `state.AddL1Chain` rather
  than `state.AddChain`
- The critical entry for backwards compatibility is chains/{subnetID}/list/{txID}. Previously this prefix only ever
  stored txIDs belonging to `CreateChainTx` transactions. `CreateL1Tx` stores its own `txID` under this same prefix, meaning `GetChains` now returns a `*txs.CreateL1Tx` instead of a `*txs.CreateChainTx` for L1s created this way. All callers of `GetChains` (`createSubnet` in `vm.go` and `Diff.Apply` in `diff.go`) were updated to handle both types via a type switch, preserving backwards compatibility with existing subnets and their `CreateChainTx` chains.
- `SetSubnetOwner` is called with an empty owner (&secp256k1fx.OutputOwners{}) on the new subnet. L1 subnets have no traditional PoA owner, but the `GetSubnet` API currently requires an owner entry in state to return the subnet. This is temporary and it will be removed when service.go is deprecated.

> Note: The transaction is currently gated behind the Etna upgrade check (IsEtnaActivated) in the executor, as this was the active upgrade at the time of implementation. Prior to merging into production, this gate should be updated to reflect the actual target upgrade under which CreateL1Tx will be deployed. Additionally, the codec registration should be verified to align with that same upgrade (currently registered under RegisterEtnaTypes).

## Design Decisions:

After reading through ACP-191, I Encountered a problem:

Background: The state has a prefixed key value store structure. In the chains/subnetID prefix we store a list of txIDs. Currently these transaction IDs are ids from `CreateChainTx`. 

Problem: These tx IDs represent the ID of the chain they are creating. However when we are creating an L1, we never issue `CreateChainTxs`. So how do we go about storing the chain information without breaking backwards compatibility? Additionally, how will we get the chain info?

 Solution: `GetChains` in `state.go` returns a list of transactions that currently are only `CreateChainTxs`. `GetChains` is used in 2 places, `createSubnet` and `service.go`. For now we do not worry about `service.go`, because it seems that the request/response is deprecated. `CreateSubnet` is used by our node to start running the chains defined by the subnet. 

It is only in `createSubnet` where we cast the txs returned by `GetChains` to `CreateChainTx`. We then use the data from `createChainTx` to actually start the chain. However, we don’t need the entire tx metadata to start the chain. The required parts of chain creation should be both in `CreateChainTx` and `CreateL1Tx`. Therefore, in `createSubnet` we can branch off the type of transaction and create a common struct that extracts the important chain creation details. After reviewing the existing code, I found the `ChainParameters` struct that has all of the chain creation details we need and I used it to implement `createL1Chain` similar to `CreateChain`. 



## Backwards Compatibility

`CreateL1Tx` is additive, it does not modify or replace the existing three-transaction flow (`CreateSubnetTx` + `CreateChainTx` + `ConvertSubnetToL1Tx`). Existing subnets and chains created via that flow are completely unaffected.

The only state-layer change is that `GetChains` may now return a mix of `*txs.CreateChainTx` and `*txs.CreateL1Tx` transactions for a given subnet. All callers of `GetChains` (`createSubnet` in `vm.go` and `Diff.Apply` in `diff.go`) were updated with a type switch to handle both, preserving full read compatibility with existing chain records.


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
After getting familiar with the codebase, I started working on the design. See "Design Decisions" and "Key Implementation Details" for more details.


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
  19. vm_test.go 

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





