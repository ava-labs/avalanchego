// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package corethgen generates the corethtest fixture: a deterministic C-Chain
// history crossing every scheduled network upgrade, for tests that need
// realistic synchronous blocks and state; e.g. tests of the coreth-to-SAE
// transition.
//
// Generation lives here because it needs a live coreth VM. TestFixtureUpToDate
// activates each upgrade at a distinct timestamp, accepts at least one block
// per upgrade exercising an observable change of that upgrade, records the
// JSON-RPC responses coreth serves for the resulting chain, and dumps the VM's
// entire database. The result is written to the corethtest package that
// consumers import, and read back from it to detect a stale fixture.
//
//go:generate go test -run TestFixtureUpToDate -update .
package corethgen

// Heights of fixture blocks that the generator singles out, asserted during
// generation so they cannot silently drift.
var (
	// NativeAssetCallBlocks carry a functional nativeAssetCall, whose nested
	// EVM call makes tracing fail with [NativeAssetCallTraceError] — in
	// coreth and in any replaying VM.
	NativeAssetCallBlocks = []uint64{8, 12}

	// DeprecatedNativeAssetCallBlock's nativeAssetCall ran while the
	// precompile was deprecated, giving the fixture's only failed receipt.
	DeprecatedNativeAssetCallBlock uint64 = 11

	// SendWarpMessageBlock's sendWarpMessage logs the warp precompile's
	// SendWarpMessage event.
	SendWarpMessageBlock uint64 = 16
)

// NativeAssetCallTraceError is the exact error from the debug_trace*
// endpoints on [NativeAssetCallBlocks].
const NativeAssetCallTraceError = "incorrect number of top-level calls"
