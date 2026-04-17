// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package platformvm implements the Avalanche P-Chain (Platform Chain).
//
// The P-Chain is the metadata blockchain that coordinates validators across all
// Avalanche subnets. It is responsible for:
//
//   - Tracking the validator set for the Primary Network and all subnets.
//   - Processing staking transactions that add or remove validators and
//     delegators.
//   - Creating new subnets and blockchains.
//   - Managing network upgrades through activation heights.
//
// # Architecture
//
// The top-level [VM] struct implements the [snow/engine/snowman.VM] interface
// and is the entry point registered with the VM manager. It embeds the block
// builder, network gossip layer, and validator state, and delegates transaction
// execution to the subpackages described below.
//
// Key subpackages:
//
//   - block/: P-Chain block types (StandardBlock, ProposalBlock, CommitBlock,
//     AbortBlock, BanffStandardBlock, BanffProposalBlock) and their execution.
//   - txs/: All P-Chain transaction types and codec registration.
//   - txs/executor/: Stateless and stateful transaction verification, and
//     transaction execution against chain state.
//   - state/: Persistent chain state backed by a key-value database. Tracks
//     UTXOs, validators, subnets, blocks, and pending staker sets.
//   - network/: P2P gossip of unconfirmed transactions.
//   - api/: JSON-RPC service handlers exposed over HTTP.
//   - config/: Static (genesis-derived) and execution (operator-configured)
//     P-Chain configuration.
//   - validators/: Validator set construction and height-indexed snapshots.
//   - utxo/: UTXO verification and spending helpers shared across tx types.
//   - genesis/: Genesis state parsing and validation.
//   - reward/: Staking reward calculation.
//   - metrics/: Prometheus metrics for the VM and its components.
//   - warp/: Avalanche Warp Messaging signature aggregation for the P-Chain.
package platformvm
