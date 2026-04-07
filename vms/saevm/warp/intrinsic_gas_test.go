// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	cmath "github.com/ava-labs/libevm/common/math"
	libevmcore "github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	corethcore "github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// Isolated addresses for mocks: [params.TestChainConfig.Rules] pre-populates
// Predicaters from registered modules; low addresses like 0x01 may collide.
var (
	addrTestPredicater0 = common.HexToAddress("0x00000000000000000000000000000000a11c0de0")
	addrTestNormal0     = common.HexToAddress("0x00000000000000000000000000000000b00b0b00")
)

// intrinsicGasDeltaFuzzSeed is a readable corpus entry; use [encodeIntrinsicGasDeltaFuzzInput] for f.Add.
type intrinsicGasDeltaFuzzSeed struct {
	isPredicater   []bool
	storageKeyLens []uint16
	signerLen      uint16
}

// encodeIntrinsicGasDeltaFuzzInput converts a seed to the byte form expected by [FuzzAccessListIntrinsicGasDelta]:
//   - isPredicater: one byte per bool (0 or 1), index i selects tuple i (with wrap via i%len in the fuzz body).
//   - storageKeyLens: little-endian uint16s, two bytes per tuple length.
func encodeIntrinsicGasDeltaFuzzInput(c intrinsicGasDeltaFuzzSeed) (isPred []byte, storageKeyByteLens []byte, signerLen uint16) {
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
	return isPred, storageKeyByteLens, c.signerLen
}

func FuzzAccessListIntrinsicGasDelta(f *testing.F) {
	for _, c := range []intrinsicGasDeltaFuzzSeed{
		{isPredicater: []bool{true}, storageKeyLens: []uint16{90}, signerLen: 5},
	} {
		a, b, s := encodeIntrinsicGasDeltaFuzzInput(c)
		f.Add(a, b, s)
	}

	f.Fuzz(func(t *testing.T, isPredicater []byte, storageKeyLens []byte, signerLen uint16) {
		if signerLen == 0 {
			t.Skip()
		}
		predicater := &testPredicater{signerLen: signerLen}

		rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
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

		t.Logf("access list length: %d", len(al))
		t.Logf("isPredicater: %v", isPredicater)
		t.Logf("storageKeyLenByteInput: %v", storageKeyLens)
		t.Logf("signerLen: %d", signerLen)
		t.Logf("IsGraniteActivated: %v", rulesExtra.IsGraniteActivated())

		delta, deltaErr := AccessListIntrinsicGasDelta(rules, al)
		libevmTotal, libevmErr := libevmcore.IntrinsicGas(nil, al, false, rules.IsHomestead, rules.IsIstanbul, rules.IsShanghai)
		corethTotal, corethErr := corethcore.IntrinsicGas(nil, al, false, rules)
		t.Logf("libevmTotal: %d", libevmTotal)
		t.Logf("corethTotal: %d", corethTotal)
		t.Logf("delta: %d", delta)
		require.ErrorIs(t, libevmErr, corethErr)
		require.ErrorIs(t, deltaErr, corethErr)
		if deltaErr != nil {
			return
		}
		require.Equal(t, corethTotal, libevmTotal+delta)
	})
}

type testPredicater struct {
	signerLen uint16
}

func (p *testPredicater) PredicateGas(pred predicate.Predicate, rules precompileconfig.Rules) (uint64, error) {
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
	signerGas, overflow := cmath.SafeMul(uint64(p.signerLen), config.PerWarpSigner)
	if overflow {
		return 0, fmt.Errorf("overflow calculating gas cost for %d signers", p.signerLen)
	}
	totalGas, overflow = cmath.SafeAdd(totalGas, signerGas)
	if overflow {
		return 0, fmt.Errorf("overflow adding gas cost for %d signers", p.signerLen)
	}
	return totalGas, nil
}

func (_ *testPredicater) VerifyPredicate(_ *precompileconfig.PredicateContext, _ predicate.Predicate) error {
	return nil
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
