// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

// forkHeader is a header shaped like the ones produced by a given C-Chain
// upgrade, together with the RLP list fields that follow ExtDataHash on the
// wire. The wire format is spelled out here independently of
// [HeaderExtra.rlpFields] because every historical block hash depends on it.
type forkHeader struct {
	name   string
	header *types.Header
	extra  *HeaderExtra
	tail   []any
}

// legacyHeader returns a pre-Apricot-Phase-3 header: the fifteen go-ethereum
// fields followed by ExtDataHash, and nothing else.
func legacyHeader() (*types.Header, *HeaderExtra) {
	header := &types.Header{
		ParentHash:  common.Hash{1},
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.Address{3},
		Root:        common.Hash{4},
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{7},
		Difficulty:  big.NewInt(1),
		Number:      big.NewInt(9),
		GasLimit:    8_000_000,
		GasUsed:     21_000,
		Time:        1_600_000_000,
		Extra:       []byte{13, 14},
		MixDigest:   common.Hash{14},
		Nonce:       types.BlockNonce{15},
	}
	extra := &HeaderExtra{ExtDataHash: common.Hash{21}}
	return header, extra
}

func forkHeaders() []forkHeader {
	var forks []forkHeader
	add := func(name string, set func(*types.Header, *HeaderExtra) []any) {
		header, extra := legacyHeader()
		var tail []any
		if len(forks) > 0 {
			prev := forks[len(forks)-1]
			header, extra = legacyHeaderFrom(prev.header, prev.extra)
			tail = append(tail, prev.tail...)
		}
		tail = append(tail, set(header, extra)...)
		forks = append(forks, forkHeader{name: name, header: header, extra: extra, tail: tail})
	}

	add("pre-AP3", func(*types.Header, *HeaderExtra) []any { return nil })
	add("AP3 base fee", func(h *types.Header, _ *HeaderExtra) []any {
		h.BaseFee = big.NewInt(25_000_000_000)
		return []any{h.BaseFee}
	})
	add("AP4 ext data gas used and block gas cost", func(_ *types.Header, e *HeaderExtra) []any {
		e.ExtDataGasUsed = big.NewInt(22)
		e.BlockGasCost = big.NewInt(23)
		return []any{e.ExtDataGasUsed, e.BlockGasCost}
	})
	add("Etna blob fields", func(h *types.Header, _ *HeaderExtra) []any {
		h.BlobGasUsed = utils.PointerTo[uint64](0)
		h.ExcessBlobGas = utils.PointerTo[uint64](0)
		h.ParentBeaconRoot = &common.Hash{}
		return []any{*h.BlobGasUsed, *h.ExcessBlobGas, *h.ParentBeaconRoot}
	})
	add("Granite time milliseconds and min delay excess", func(_ *types.Header, e *HeaderExtra) []any {
		e.TimeMilliseconds = utils.PointerTo[uint64](1_600_000_000_500)
		e.MinDelayExcess = utils.PointerTo(acp226.DelayExcess(25))
		return []any{*e.TimeMilliseconds, uint64(*e.MinDelayExcess)}
	})
	add("Helicon target and min price exponents", func(_ *types.Header, e *HeaderExtra) []any {
		e.TargetExponent = utils.PointerTo(dynamic.TargetExponent(26))
		e.MinPriceExponent = utils.PointerTo(dynamic.PriceExponent(27))
		return []any{uint64(*e.TargetExponent), uint64(*e.MinPriceExponent)}
	})
	add("settled fields", func(_ *types.Header, e *HeaderExtra) []any {
		e.SettledHeight = utils.PointerTo[uint64](28)
		e.SettledGasUnix = utils.PointerTo[uint64](29)
		e.SettledGasNumerator = utils.PointerTo[uint64](30)
		e.SettledExcess = utils.PointerTo[uint64](31)
		return []any{*e.SettledHeight, *e.SettledGasUnix, *e.SettledGasNumerator, *e.SettledExcess}
	})
	return forks
}

// legacyHeaderFrom deep-copies a header and its extra without going through
// [types.CopyHeader], so the tests do not depend on [HeaderExtra.PostCopy].
func legacyHeaderFrom(h *types.Header, e *HeaderExtra) (*types.Header, *HeaderExtra) {
	hc := *h
	hc.Extra = append([]byte(nil), h.Extra...)
	ec := *e
	return &hc, &ec
}

// wantHeaderRLP is the independent specification of the header wire format:
// an RLP list of the fifteen go-ethereum fields, then ExtDataHash, then the
// fork-specific trailing fields with nothing after the last one that is set.
func wantHeaderRLP(t *testing.T, h *types.Header, e *HeaderExtra, tail []any) []byte {
	t.Helper()
	fields := []any{
		h.ParentHash, h.UncleHash, h.Coinbase, h.Root, h.TxHash, h.ReceiptHash,
		h.Bloom, h.Difficulty, h.Number, h.GasLimit, h.GasUsed, h.Time, h.Extra,
		h.MixDigest, h.Nonce, e.ExtDataHash,
	}
	fields = append(fields, tail...)
	want, err := rlp.EncodeToBytes(fields)
	require.NoError(t, err, "rlp.EncodeToBytes(fields)")
	return want
}

func TestHeaderRLPByFork(t *testing.T) {
	t.Parallel()

	for _, fork := range forkHeaders() {
		t.Run(fork.name, func(t *testing.T) {
			t.Parallel()

			want := wantHeaderRLP(t, fork.header, fork.extra, fork.tail)
			header := WithHeaderExtra(fork.header, fork.extra)

			got, err := rlp.EncodeToBytes(header)
			require.NoError(t, err, "rlp.EncodeToBytes(header)")
			require.Equal(t, want, got, "header RLP")

			content, _, err := rlp.SplitList(got)
			require.NoError(t, err, "rlp.SplitList(header RLP)")
			n, err := rlp.CountValues(content)
			require.NoError(t, err, "rlp.CountValues(header RLP)")
			require.Equal(t, 16+len(fork.tail), n, "number of RLP list elements")

			require.Equal(t, crypto.Keccak256Hash(want), header.Hash(), "Header.Hash()")

			decoded := new(types.Header)
			require.NoError(t, rlp.DecodeBytes(got, decoded), "rlp.DecodeBytes(header RLP)")
			require.Equal(t, fork.header, decoded, "decoded header")
			require.Equal(t, fork.extra, GetHeaderExtra(decoded), "decoded HeaderExtra")
		})
	}
}

func TestHeaderRLPNilOptionalBeforeSetOptional(t *testing.T) {
	t.Parallel()

	// BaseFee and ExtDataGasUsed are unset, but BlockGasCost is set, so the
	// two preceding optional fields must be written as empty strings and
	// therefore decode back as zero, not nil.
	header, extra := legacyHeader()
	extra.BlockGasCost = big.NewInt(23)
	want := wantHeaderRLP(t, header, extra, []any{(*big.Int)(nil), (*big.Int)(nil), extra.BlockGasCost})

	got, err := rlp.EncodeToBytes(WithHeaderExtra(header, extra))
	require.NoError(t, err, "rlp.EncodeToBytes(header)")
	require.Equal(t, want, got, "header RLP")

	decoded := new(types.Header)
	require.NoError(t, rlp.DecodeBytes(got, decoded), "rlp.DecodeBytes(header RLP)")
	require.Equal(t, big.NewInt(0), decoded.BaseFee, "decoded BaseFee")
	decodedExtra := GetHeaderExtra(decoded)
	require.Equal(t, big.NewInt(0), decodedExtra.ExtDataGasUsed, "decoded ExtDataGasUsed")
	require.Equal(t, extra.BlockGasCost, decodedExtra.BlockGasCost, "decoded BlockGasCost")
}

func TestHeaderRLPRejectsTrailingValue(t *testing.T) {
	t.Parallel()

	header, extra := legacyHeader()
	encoded, err := rlp.EncodeToBytes(WithHeaderExtra(header, extra))
	require.NoError(t, err, "rlp.EncodeToBytes(header)")

	err = rlp.DecodeBytes(append(encoded, 0x80), new(types.Header))
	require.ErrorIs(t, err, rlp.ErrMoreThanOneValue, "rlp.DecodeBytes(header RLP + trailing value)")
}

func TestHeaderRLPRejectsExtraListElement(t *testing.T) {
	t.Parallel()

	forks := forkHeaders()
	last := forks[len(forks)-1]
	encoded := wantHeaderRLP(t, last.header, last.extra, append(last.tail, uint64(32)))

	err := rlp.DecodeBytes(encoded, new(types.Header))
	require.EqualError(t, err, "rlp: call of ListEnd not positioned at EOL",
		"rlp.DecodeBytes(header RLP with one element past SettledExcess)")
}

func TestHeaderRLPRejectsTooFewElements(t *testing.T) {
	t.Parallel()

	err := rlp.DecodeBytes([]byte{0xc0}, new(types.Header))
	require.ErrorIs(t, err, errTooFewHeaderFields, "rlp.DecodeBytes(empty list)")

	// A header with too few elements nested in a list MUST fail the whole
	// decode rather than being read as the end of the enclosing list, which is
	// how libevm's slice decoder interprets a bare [rlp.EOL] from an element.
	var uncles []*types.Header
	err = rlp.DecodeBytes([]byte{0xc3, 0xc0, 0x30, 0x30}, &uncles)
	require.ErrorIs(t, err, errTooFewHeaderFields, "rlp.DecodeBytes(uncles with an empty-list header)")
}

func TestHeaderRPCMarshalOnlySetFields(t *testing.T) {
	t.Parallel()

	for _, fork := range forkHeaders() {
		t.Run(fork.name, func(t *testing.T) {
			t.Parallel()

			m := make(map[string]any)
			fork.extra.PostRPCMarshal(fork.header, m)

			want := map[string]bool{
				"extDataHash":           true,
				"extDataGasUsed":        fork.extra.ExtDataGasUsed != nil,
				"blockGasCost":          fork.extra.BlockGasCost != nil,
				"timestampMilliseconds": fork.extra.TimeMilliseconds != nil,
				"minDelayExcess":        fork.extra.MinDelayExcess != nil,
				"targetExponent":        fork.extra.TargetExponent != nil,
				"minPriceExponent":      fork.extra.MinPriceExponent != nil,
				"settledHeight":         fork.extra.SettledHeight != nil,
				"settledGasUnix":        fork.extra.SettledGasUnix != nil,
				"settledGasNumerator":   fork.extra.SettledGasNumerator != nil,
				"settledExcess":         fork.extra.SettledExcess != nil,
			}
			for key, present := range want {
				_, ok := m[key]
				require.Equalf(t, present, ok, "PostRPCMarshal key %q present", key)
			}
			require.Len(t, m, len(want)-countFalse(want), "PostRPCMarshal keys")
		})
	}
}

func TestHeaderJSONHashAndExtras(t *testing.T) {
	t.Parallel()

	for _, fork := range forkHeaders() {
		t.Run(fork.name, func(t *testing.T) {
			t.Parallel()

			header := WithHeaderExtra(fork.header, fork.extra)
			encoded, err := json.Marshal(header)
			require.NoError(t, err, "json.Marshal(header)")

			var m map[string]any
			require.NoError(t, json.Unmarshal(encoded, &m), "json.Unmarshal(header JSON)")
			require.Equal(t, header.Hash().Hex(), m["hash"], `JSON "hash"`)
			require.Equal(t, fork.extra.ExtDataHash.Hex(), m["extDataHash"], `JSON "extDataHash"`)
			if fork.extra.BlockGasCost != nil {
				require.Equal(t, (*hexutil.Big)(fork.extra.BlockGasCost).String(), m["blockGasCost"], `JSON "blockGasCost"`)
			}

			decoded := new(types.Header)
			require.NoError(t, json.Unmarshal(encoded, decoded), "json.Unmarshal(header JSON, *types.Header)")
			require.Equal(t, fork.header, decoded, "decoded header")
			require.Equal(t, fork.extra, GetHeaderExtra(decoded), "decoded HeaderExtra")
		})
	}
}

func countFalse(m map[string]bool) int {
	n := 0
	for _, v := range m {
		if !v {
			n++
		}
	}
	return n
}
