// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package synchronoustest provides a deterministic, pre-generated C-Chain
// history crossing every pre-SAE network upgrade, for tests that need realistic
// synchronous blocks.
package synchronoustest

import (
	"encoding/json"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	_ "embed"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/upgrade"
)

// A Fixture is a complete, deterministic, C-Chain history.
type Fixture struct {
	// Genesis is the JSON-encoded [core.Genesis] passed to VM Initialize.
	Genesis json.RawMessage `json:"genesis"`
	// Upgrades is the [upgrade.Config] the chain was generated with.
	Upgrades upgrade.Config `json:"upgrades"`
	// Blocks holds the genesis block followed by every accepted block, in
	// height order.
	Blocks []Block `json:"blocks"`
	// RPCCalls holds JSON-RPC calls and the responses the synchronous VM
	// returned.
	RPCCalls []RPCCall `json:"rpcCalls"`
	// Database holds every key-value pair of the VM's database after all
	// Blocks were accepted.
	Database map[string]hexutil.Bytes `json:"database"`
}

//go:embed fixture.json
var fixtureJSON []byte

// Load decodes fixture.json, committed alongside this package.
//
// # Chain
//
// The fixture starts with no upgrade active and includes at least one block
// under every network upgrade through Granite, so every historical ruleset is
// exercised. Each entry names what the upgrade changed and what its blocks do
// with it.
//
//   - No upgrades: the launch rules, a 470 gwei minimum gas price, gas refunds
//     applied, and a gas limit inherited from genesis. A transfer and a storage
//     set-and-clear whose SSTORE refund lowers the gas charged.
//   - ApricotPhase1: lowers the minimum gas price to a fixed 225 gwei, still
//     with no base fee, fixes the gas limit at 8M, and discards gas refunds. A
//     counter-contract deploy, a transfer, and a storage set-and-clear accruing
//     an SSTORE refund the rules then drop.
//   - ApricotPhase2: activates Berlin, adding EIP-2930 access-list
//     transactions, and introduces the nativeAsset precompiles. A legacy
//     transfer in the activation block, then an access-list transfer.
//   - ApricotPhase3: activates London, so a block carries a base fee that moves
//     with demand and dynamic-fee (EIP-1559) transactions become valid. A
//     legacy transfer in the activation block, then cross-chain imports of AVAX
//     and of an ANT under the pre-AP5 extData encoding, the latter alongside a
//     dynamic-fee transfer.
//   - ApricotPhase4: adds the extDataGasUsed and blockGasCost header fields. An
//     AVAX export.
//   - ApricotPhase5: switches the extData encoding and allows more than one
//     cross-chain transaction per block. Two imports batched into one block.
//   - ApricotPhasePre6: deprecates nativeAssetBalance and nativeAssetCall, so
//     calls to them fail. A nativeAssetCall carrying the fixture's only failing
//     receipt.
//   - ApricotPhase6: restores both precompiles. A nativeAssetCall that
//     succeeds.
//   - ApricotPhasePost6: no C-Chain rule change, so the precompiles stay
//     functional. A plain transfer, so the fixture still spans the upgrade.
//   - Banff: deprecates nativeAssetBalance and nativeAssetCall permanently,
//     Granite included, and restricts cross-chain transfers to AVAX. An AVAX
//     export.
//   - Cortina: raises the gas limit to 15M. A counter increment.
//   - Durango: activates the warp precompile and access-list predicates. A
//     sendWarpMessage, then a getVerifiedWarpMessage behind a predicate whose
//     results land in the header extra.
//   - Etna: adds Cancun's blobGasUsed, excessBlobGas and parentBeaconRoot
//     header fields. A dynamic-fee transfer.
//   - Fortuna: prefixes the extra data with the ACP-176 fee state. A
//     dynamic-fee transfer.
//   - Granite: adds the timeMilliseconds and minDelayExcess header fields and
//     the ACP-226 minimum block delay. Two blocks, the second paced by that
//     delay and carrying three transactions so tracing the last replays the
//     first two.
//
// # Recorded RPC responses
//
// How the synchronous VM answered queries about the chain. Every height from
// genesis to the tip is queried, and the accounts queried at each are the two
// funded EOAs, the transfer recipient, the counter contract, and the blackhole
// coinbase that burned fees accrue to. Slot 0 is the counter's only storage slot
// and is empty in every other account.
//
//   - eth_getBalance: each account at each height.
//   - eth_getTransactionCount: each account at each height.
//   - eth_getCode: each account at each height.
//   - eth_getStorageAt: slot 0 of each account at each height.
//   - eth_getProof: each account at each height, proving slot 0.
//   - eth_call: the counter contract at each height, with call data that makes
//     it return the slot rather than increment it.
//   - eth_callDetailed: that same call at each height.
//   - eth_getBlockReceipts: each height.
//   - eth_getTransactionReceipt: each transaction hash.
//   - eth_getTransactionByHash: each transaction hash.
//   - eth_getLogs: four filter shapes, one height at a time, the full height
//     range, the block hash of the sendWarpMessage block, and the warp
//     precompile's address over the full range.
//   - debug_intermediateRoots: each block hash, yielding the state root after
//     each of that block's transactions.
//   - debug_traceBlockByNumber: each height, under both block tracers.
//   - debug_traceBlockByHash: each block hash, under both block tracers.
//   - debug_traceBlock: each block's RLP, under both block tracers. The RLP
//     form takes its own path to the executed base fee, which only a prestate
//     reveals.
//   - debug_traceTransaction: each transaction hash, under all three tracers.
//
// The two block tracers are a callTracer and a prestateTracer, and
// debug_traceTransaction adds the prestateTracer in diff mode. A callTracer
// reports the tree of EVM calls, a prestateTracer the state each transaction
// read, and diff mode the state it changed. The prestates pin intra-block
// ordering and the executed base fee, because they show the coinbase accruing
// each transaction's fees in turn.
//
// Errors count as responses. Tracing genesis fails in the synchronous VM, so
// the fixture records the failure and a replaying VM has to reproduce it.
func Load(tb testing.TB) *Fixture {
	tb.Helper()

	fx := new(Fixture)
	require.NoError(tb, json.Unmarshal(fixtureJSON, fx), "unmarshalling fixture")
	return fx
}

// An RPCCall is a JSON-RPC request and the response the synchronous VM returned
// for it. Exactly one of Result or Error should be set.
type RPCCall struct {
	// Name describes what the call covers and identifies it in test output.
	Name   string            `json:"name"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
	// Result is a successful response's result.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is an error response's message.
	Error string `json:"error,omitempty"`
}

// Args returns [RPCCall.Params] as a slice of [any] values. It can easily be
// used with an ethclient.
func (r *RPCCall) Args() []any {
	args := make([]any, len(r.Params))
	for i, p := range r.Params {
		args[i] = p
	}
	return args
}

// CoreGenesis decodes [Fixture.Genesis].
func (f *Fixture) CoreGenesis(tb testing.TB) core.Genesis {
	tb.Helper()

	var genesis core.Genesis
	require.NoError(tb, json.Unmarshal(f.Genesis, &genesis), "unmarshalling genesis")
	return genesis
}

// WriteDatabase writes every [Fixture.Database] key-value pair to db,
// recreating the database a synchronous node would hand to its successor VM.
func (f *Fixture) WriteDatabase(tb testing.TB, db database.KeyValueWriter) {
	tb.Helper()

	for keyHex, value := range f.Database {
		key, err := hexutil.Decode(keyHex)
		require.NoError(tb, err, "decoding key %s", keyHex)
		require.NoError(tb, db.Put(key, value), "writing entry %s", keyHex)
	}
}

// A Block is an accepted block used in the fixture chain.
type Block struct {
	// Fork names the network upgrade whose rules the block was built under.
	Fork string `json:"fork"`
	// Description explains what the block exercises.
	Description string      `json:"description"`
	Number      uint64      `json:"number"`
	Hash        common.Hash `json:"hash"`
	// Decoding the block requires the C-Chain libevm extras to be registered.
	RLP hexutil.Bytes `json:"rlp"`
}

// EthBlock decodes [Block.RLP]; the C-Chain libevm extras MUST be registered.
func (b Block) EthBlock(tb testing.TB) *types.Block {
	tb.Helper()

	eth := new(types.Block)
	require.NoError(tb, rlp.DecodeBytes(b.RLP, eth), "rlp.DecodeBytes(block %d)", b.Number)
	return eth
}
