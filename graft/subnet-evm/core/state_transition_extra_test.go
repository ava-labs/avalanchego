// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"

	cmath "github.com/ava-labs/libevm/common/math"
	libevmcore "github.com/ava-labs/libevm/core"
)

// TestIntrinsicGasEquivalenceErrors verifies that libevm/core.IntrinsicGas with
// hooks produces identical error results to the legacy Subnet-EVM implementation.
// Happy-path equivalence is covered by [FuzzAccessListIntrinsicGasEquivalence].
func TestIntrinsicGasEquivalenceErrors(t *testing.T) {
	predicateAddr := common.Address{0x01}
	errPredicateGas := errors.New("predicate gas error")

	tests := []struct {
		name       string
		accessList types.AccessList
		predicater *testPredicater
	}{
		{
			name: "predicate_gas_error",
			accessList: types.AccessList{
				{Address: predicateAddr, StorageKeys: []common.Hash{{1}}},
			},
			predicater: newStaticPredicater(0, errPredicateGas),
		},
		{
			name: "predicate_gas_overflow",
			accessList: types.AccessList{
				{Address: predicateAddr, StorageKeys: []common.Hash{{1}}},
				{Address: common.Address{0x03}, StorageKeys: []common.Hash{{2}}},
			},
			predicater: newStaticPredicater(math.MaxUint64, nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
			rulesExtra := params.GetRulesExtra(rules)

			rulesExtra.Predicaters = make(map[common.Address]precompileconfig.Predicater)
			for _, tuple := range tt.accessList {
				rulesExtra.Predicaters[tuple.Address] = tt.predicater
			}

			_, legacyErr := legacyIntrinsicGas(nil, tt.accessList, false, rules)
			_, libevmErr := libevmcore.IntrinsicGas(nil, tt.accessList, false, rules)

			require.ErrorIs(legacyErr, libevmErr)
		})
	}
}

// Isolated addresses for mocks: [params.TestChainConfig.Rules] pre-populates
// Predicaters from registered modules; low addresses like 0x01 may collide.
var (
	addrTestPredicater0 = common.HexToAddress("0x00000000000000000000000000000000a11c0de0")
	addrTestNormal0     = common.HexToAddress("0x00000000000000000000000000000000b00b0b00")
)

// fuzzChainConfigs covers the fork boundaries that affect intrinsic gas:
//   - TestSubnetEVMChainConfig: pre-Durango (no init-code word gas)
//   - TestDurangoChainConfig: Durango + Shanghai
//   - TestChainConfig: latest (Cancun)
var fuzzChainConfigs = []*params.ChainConfig{
	params.TestSubnetEVMChainConfig,
	params.TestDurangoChainConfig,
	params.TestChainConfig,
}

// intrinsicGasFuzzSeed is a readable corpus entry; use [encodeIntrinsicGasFuzzInput] for f.Add.
type intrinsicGasFuzzSeed struct {
	configIdx          uint8
	isPredicater       []bool
	storageKeyLens     []uint16
	signerLen          uint16
	data               []byte
	isContractCreation bool
}

// encodeIntrinsicGasFuzzInput converts a seed to the byte form expected by [FuzzAccessListIntrinsicGasEquivalence]:
//   - isPredicater: one byte per bool (0 or 1), index i selects tuple i (with wrap via i%len in the fuzz body).
//   - storageKeyLens: little-endian uint16s, two bytes per tuple length.
func encodeIntrinsicGasFuzzInput(c intrinsicGasFuzzSeed) (configIdx uint8, isPred []byte, storageKeyByteLens []byte, signerLen uint16, data []byte, isContractCreation bool) {
	isPred = make([]byte, len(c.isPredicater))
	for i, b := range c.isPredicater {
		if b {
			isPred[i] = 1
		}
	}
	storageKeyByteLens = make([]byte, 2*len(c.storageKeyLens))
	for i, v := range c.storageKeyLens {
		binary.LittleEndian.PutUint16(storageKeyByteLens[2*i:], v)
	}
	return c.configIdx, isPred, storageKeyByteLens, c.signerLen, c.data, c.isContractCreation
}

func FuzzAccessListIntrinsicGasEquivalence(f *testing.F) {
	seeds := []intrinsicGasFuzzSeed{
		// Predicater with storage keys
		{isPredicater: []bool{true}, storageKeyLens: []uint16{90}, signerLen: 5},
		// No access list tuples
		{signerLen: 1},
		// Single regular address with keys
		{isPredicater: []bool{false}, storageKeyLens: []uint16{3}, signerLen: 1},
		// Multiple regular addresses
		{isPredicater: []bool{false, false}, storageKeyLens: []uint16{1, 2}, signerLen: 1},
		// Mixed predicater and regular
		{isPredicater: []bool{true, false}, storageKeyLens: []uint16{1, 3}, signerLen: 5},
		// Large access list
		{isPredicater: []bool{false, false, false, false}, storageKeyLens: []uint16{5, 3, 2, 0}, signerLen: 1},
		// Zero-byte data
		{isPredicater: []bool{false}, storageKeyLens: []uint16{1}, signerLen: 1, data: []byte{0, 0, 0, 0}},
		// Non-zero-byte data
		{isPredicater: []bool{false}, storageKeyLens: []uint16{1}, signerLen: 1, data: []byte{1, 2, 3, 4}},
		// Mixed data
		{isPredicater: []bool{false}, storageKeyLens: []uint16{1}, signerLen: 1, data: []byte{0, 1, 0, 2, 3}},
		// Contract creation with access list
		{isPredicater: []bool{false}, storageKeyLens: []uint16{1}, signerLen: 1, isContractCreation: true},
		// Contract creation with data (exercises Durango init-code word gas)
		{isPredicater: []bool{false}, storageKeyLens: []uint16{1}, signerLen: 1, data: make([]byte, 100), isContractCreation: true},
		// Data with predicater
		{isPredicater: []bool{true}, storageKeyLens: []uint16{2}, signerLen: 3, data: []byte{0, 0xff, 0, 0xff}},
	}
	for ci := range fuzzChainConfigs {
		for _, s := range seeds {
			s.configIdx = uint8(ci)
			cfgIdx, a, b, sl, d, cc := encodeIntrinsicGasFuzzInput(s)
			f.Add(cfgIdx, a, b, sl, d, cc)
		}
	}

	f.Fuzz(func(t *testing.T, configIdx uint8, isPredicater []byte, storageKeyLens []byte, signerLen uint16, data []byte, isContractCreation bool) {
		if signerLen == 0 {
			t.Skip()
		}
		cfg := fuzzChainConfigs[int(configIdx)%len(fuzzChainConfigs)]
		predicater := newWarpPredicater(signerLen)

		rules := cfg.Rules(common.Big0, params.IsMergeTODO, 0)
		rulesExtra := params.GetRulesExtra(rules)
		rulesExtra.Predicaters[addrTestPredicater0] = predicater

		keyLens := decodeUint16s(storageKeyLens)
		al := make(types.AccessList, 0, len(keyLens))
		for i, kl := range keyLens {
			addr := addrTestNormal0
			if len(isPredicater) > 0 && isPredicater[i%len(isPredicater)]&1 == 1 {
				addr = addrTestPredicater0
			}
			al = append(al, types.AccessTuple{
				Address:     addr,
				StorageKeys: hashRepeat(int(kl)),
			})
			t.Logf("(isPredicater[%d] == %v", i, addr == addrTestPredicater0)
			t.Logf("(storageKeyLens[%d] == %d)", i, kl)
		}

		t.Logf("config: %d (IsDurango=%v)", configIdx%uint8(len(fuzzChainConfigs)), rulesExtra.IsDurango)
		t.Logf("access list length: %d", len(al))
		t.Logf("isPredicater: %v", isPredicater)
		t.Logf("storageKeyLenByteInput: %v", storageKeyLens)
		t.Logf("signerLen: %d", signerLen)
		t.Logf("data length: %d", len(data))
		t.Logf("isContractCreation: %v", isContractCreation)

		libevmTotal, libevmErr := libevmcore.IntrinsicGas(data, al, isContractCreation, rules)
		legacyTotal, legacyErr := legacyIntrinsicGas(data, al, isContractCreation, rules)
		t.Logf("libevmTotal: %d", libevmTotal)
		t.Logf("legacyTotal: %d", legacyTotal)
		require.ErrorIs(t, libevmErr, legacyErr)
		require.Equal(t, legacyTotal, libevmTotal)
	})
}

// testPredicater implements [precompileconfig.Predicater] for tests by
// delegating PredicateGas to a caller-supplied function.
type testPredicater struct {
	predicateGasF func(predicate.Predicate, precompileconfig.Rules) (uint64, error)
}

func (p *testPredicater) PredicateGas(pred predicate.Predicate, rules precompileconfig.Rules) (uint64, error) {
	return p.predicateGasF(pred, rules)
}

func (*testPredicater) VerifyPredicate(*precompileconfig.PredicateContext, predicate.Predicate) error {
	return nil
}

func newWarpPredicater(signerLen uint16) *testPredicater {
	return &testPredicater{
		predicateGasF: func(pred predicate.Predicate, rules precompileconfig.Rules) (uint64, error) {
			config := warp.CurrentGasConfig(rules)
			totalGas := config.VerifyPredicateBase
			bytesGasCost, overflow := cmath.SafeMul(config.PerWarpMessageChunk, uint64(len(pred)))
			if overflow {
				return 0, fmt.Errorf("overflow calculating gas cost for %d warp message chunks", len(pred))
			}
			totalGas, overflow = cmath.SafeAdd(totalGas, bytesGasCost)
			if overflow {
				return 0, fmt.Errorf("overflow adding gas cost for %d warp message chunks", len(pred))
			}
			signerGas, overflow := cmath.SafeMul(uint64(signerLen), config.PerWarpSigner)
			if overflow {
				return 0, fmt.Errorf("overflow calculating gas cost for %d signers", signerLen)
			}
			totalGas, overflow = cmath.SafeAdd(totalGas, signerGas)
			if overflow {
				return 0, fmt.Errorf("overflow adding gas cost for %d signers", signerLen)
			}
			return totalGas, nil
		},
	}
}

func newStaticPredicater(gas uint64, err error) *testPredicater {
	return &testPredicater{
		predicateGasF: func(predicate.Predicate, precompileconfig.Rules) (uint64, error) {
			return gas, err
		},
	}
}

func decodeUint16s(b []byte) []uint16 {
	n := len(b) / 2
	out := make([]uint16, n)
	for i := 0; i < n; i++ {
		out[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return out
}

func hashRepeat(n int) []common.Hash {
	keys := make([]common.Hash, n)
	for i := range keys {
		keys[i][0] = byte(i + 1)
	}
	return keys
}
