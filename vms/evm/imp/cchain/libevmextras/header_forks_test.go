// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package libevmextras

import (
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
)

// forkHeader is a header shaped like the ones produced by a given C-Chain
// upgrade, together with the RLP list fields that follow ExtDataHash on the
// wire. The wire format is spelled out here independently of
// [Header.rlpFields] because every historical block hash depends on it.
type forkHeader struct {
	name   string
	header *types.Header
	extra  *Header
	tail   []any
}

// legacyHeader returns a pre-Apricot-Phase-3 header: the fifteen go-ethereum
// fields followed by ExtDataHash, and nothing else.
func legacyHeader() (*types.Header, *Header) {
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
	extra := &Header{ExtDataHash: common.Hash{21}}
	return header, extra
}

func forkHeaders() []forkHeader {
	var (
		forks []forkHeader
		sets  []func(*types.Header, *Header) []any
	)
	add := func(name string, set func(*types.Header, *Header) []any) {
		sets = append(sets, set)
		header, extra := legacyHeader()
		var tail []any
		for _, set := range sets {
			tail = append(tail, set(header, extra)...)
		}
		forks = append(forks, forkHeader{name: name, header: header, extra: extra, tail: tail})
	}

	add("pre-AP3", func(*types.Header, *Header) []any { return nil })
	add("AP3 base fee", func(h *types.Header, _ *Header) []any {
		h.BaseFee = big.NewInt(25_000_000_000)
		return []any{h.BaseFee}
	})
	add("AP4 ext data gas used and block gas cost", func(_ *types.Header, e *Header) []any {
		e.ExtDataGasUsed = big.NewInt(22)
		e.BlockGasCost = new(big.Int).Lsh(big.NewInt(1), 70) // > 64 bits: JSON must use hexutil.Big
		return []any{e.ExtDataGasUsed, e.BlockGasCost}
	})
	add("Etna blob fields", func(h *types.Header, _ *Header) []any {
		h.BlobGasUsed = new(uint64(0))
		h.ExcessBlobGas = new(uint64(0))
		h.ParentBeaconRoot = &common.Hash{}
		return []any{*h.BlobGasUsed, *h.ExcessBlobGas, *h.ParentBeaconRoot}
	})
	add("Granite time milliseconds and min delay excess", func(_ *types.Header, e *Header) []any {
		e.TimeMilliseconds = new(uint64(1_600_000_000_500))
		e.MinDelayExcess = new(dynamic.DelayExponent(25))
		return []any{*e.TimeMilliseconds, uint64(*e.MinDelayExcess)}
	})
	add("Helicon target and min price exponents", func(_ *types.Header, e *Header) []any {
		e.TargetExponent = new(dynamic.TargetExponent(26))
		e.MinPriceExponent = new(dynamic.PriceExponent(27))
		return []any{uint64(*e.TargetExponent), uint64(*e.MinPriceExponent)}
	})
	add("settled fields", func(_ *types.Header, e *Header) []any {
		e.SettledHeight = new(uint64(28))
		e.SettledGasUnix = new(uint64(29))
		e.SettledGasNumerator = new(uint64(30))
		e.SettledExcess = new(uint64(31))
		return []any{*e.SettledHeight, *e.SettledGasUnix, *e.SettledGasNumerator, *e.SettledExcess}
	})
	return forks
}

// wantHeaderRLP is the independent specification of the header wire format:
// an RLP list of the fifteen go-ethereum fields, then ExtDataHash, then the
// fork-specific trailing fields with nothing after the last one that is set.
func wantHeaderRLP(t testing.TB, h *types.Header, e *Header, tail []any) []byte {
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
			header := WithHeader(fork.header, fork.extra)

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
			require.Equal(t, fork.extra, GetHeader(decoded), "decoded HeaderExtra")
		})
	}
}

func TestHeaderRLPNilOptionalBeforeSetOptional(t *testing.T) {
	t.Parallel()

	// BaseFee and ExtDataGasUsed are written as empty strings because
	// BlockGasCost is set, so they decode as zero, not nil.
	header, extra := legacyHeader()
	extra.BlockGasCost = big.NewInt(23)
	got, err := rlp.EncodeToBytes(WithHeader(header, extra))
	require.NoError(t, err, "rlp.EncodeToBytes(header)")

	decoded := new(types.Header)
	require.NoError(t, rlp.DecodeBytes(got, decoded), "rlp.DecodeBytes(header RLP)")
	require.Equal(t, big.NewInt(0), decoded.BaseFee, "decoded BaseFee")
	decodedExtra := GetHeader(decoded)
	require.Equal(t, big.NewInt(0), decodedExtra.ExtDataGasUsed, "decoded ExtDataGasUsed")
	require.Equal(t, extra.BlockGasCost, decodedExtra.BlockGasCost, "decoded BlockGasCost")
}

func TestHeaderEncodeRLPMatchesFields(t *testing.T) {
	t.Parallel()

	_, extra := headerWithNonZeroFields()
	for i := range len(extra.rlpFields(new(types.Header)).Optional) - 1 {
		header, extra := headerWithNonZeroFields()
		f := extra.rlpFields(header)
		nilled := reflect.ValueOf(f.Optional[i]).Elem()
		t.Run(nilled.Type().String(), func(t *testing.T) {
			nilled.SetZero()
			got, err := rlp.EncodeToBytes(header)
			require.NoError(t, err, "rlp.EncodeToBytes(header)")

			values := func(ptrs []any) []any {
				var out []any
				for _, p := range ptrs {
					out = append(out, reflect.ValueOf(p).Elem().Interface())
				}
				return out
			}
			want, err := rlp.EncodeToBytes(&rlp.Fields{Required: values(f.Required[:]), Optional: values(f.Optional[:])})
			require.NoError(t, err, "rlp.EncodeToBytes(rlp.Fields)")
			require.Equalf(t, want, got, "header RLP with optional field %d nil", i)
		})
	}
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

// TestHeaderRPCMarshal pins the RPC keys and value encodings: a legacy header
// carries only extDataHash, and the latest fork carries every field.
func TestHeaderRPCMarshal(t *testing.T) {
	t.Parallel()

	forks := forkHeaders()
	tests := []struct {
		fork forkHeader
		want string
	}{
		{
			fork: forks[0],
			want: `{"extDataHash":"0x1500000000000000000000000000000000000000000000000000000000000000"}`,
		},
		{
			fork: forks[len(forks)-1],
			want: `{
				"extDataHash":           "0x1500000000000000000000000000000000000000000000000000000000000000",
				"extDataGasUsed":        "0x16",
				"blockGasCost":          "0x400000000000000000",
				"timestampMilliseconds": "0x174876e81f4",
				"minDelayExcess":        "0x19",
				"targetExponent":        "0x1a",
				"minPriceExponent":      "0x1b",
				"settledHeight":         "0x1c",
				"settledGasUnix":        "0x1d",
				"settledGasNumerator":   "0x1e",
				"settledExcess":         "0x1f"
			}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.fork.name, func(t *testing.T) {
			t.Parallel()

			m := make(map[string]any)
			WithHeader(tt.fork.header, tt.fork.extra).PostRPCMarshal(m)
			got, err := json.Marshal(m)
			require.NoError(t, err, "json.Marshal(Header.PostRPCMarshal())")
			require.JSONEq(t, tt.want, string(got), "Header.PostRPCMarshal()")
		})
	}
}

func TestHeaderJSONHashAndExtras(t *testing.T) {
	t.Parallel()

	for _, fork := range forkHeaders() {
		t.Run(fork.name, func(t *testing.T) {
			t.Parallel()

			header := WithHeader(fork.header, fork.extra)
			encoded, err := json.Marshal(header)
			require.NoError(t, err, "json.Marshal(header)")

			var m map[string]any
			require.NoError(t, json.Unmarshal(encoded, &m), "json.Unmarshal(header JSON)")
			require.Equal(t, header.Hash().Hex(), m["hash"], `JSON "hash"`)

			decoded := new(types.Header)
			require.NoError(t, json.Unmarshal(encoded, decoded), "json.Unmarshal(header JSON, *types.Header)")
			require.Equal(t, fork.header, decoded, "decoded header")
			require.Equal(t, fork.extra, GetHeader(decoded), "decoded HeaderExtra")
		})
	}
}

// Header JSON from older nodes carries null for every unset extra field.
func TestHeaderDecodeJSONNullExtra(t *testing.T) {
	t.Parallel()

	header, extra := legacyHeader()
	encoded, err := json.Marshal(WithHeader(header, extra))
	require.NoError(t, err, "json.Marshal(header)")
	encoded = append(encoded[:len(encoded)-1], `,"blockGasCost":null,"settledHeight":null}`...)

	// Stale fields on a reused header MUST be reset, not kept.
	decoded := WithHeader(&types.Header{BaseFee: big.NewInt(16)}, &Header{SettledExcess: new(uint64)})
	require.NoError(t, json.Unmarshal(encoded, decoded), "json.Unmarshal(null extras)")
	require.Equal(t, header, decoded, "reused header after decode")
	require.Equal(t, extra, GetHeader(decoded), "null extras decode as nil")
}

// Decoding a shorter header into a receiver that held a longer one MUST reset
// the fields the shorter one omits.
func TestHeaderDecodeRLPReusedReceiver(t *testing.T) {
	t.Parallel()

	full, fullExtra := headerWithNonZeroFields()
	fullRLP, err := rlp.EncodeToBytes(WithHeader(full, fullExtra))
	require.NoError(t, err, "rlp.EncodeToBytes(full header)")
	legacy, legacyExtra := legacyHeader()
	legacyRLP, err := rlp.EncodeToBytes(WithHeader(legacy, legacyExtra))
	require.NoError(t, err, "rlp.EncodeToBytes(legacy header)")

	decoded := new(types.Header)
	require.NoError(t, rlp.DecodeBytes(fullRLP, decoded), "rlp.DecodeBytes(full header)")
	require.NoError(t, rlp.DecodeBytes(legacyRLP, decoded), "rlp.DecodeBytes(legacy header, reused receiver)")
	require.Equal(t, legacy, decoded, "reused header after legacy decode")
	require.Equal(t, legacyExtra, GetHeader(decoded), "HeaderExtra after legacy decode")
}

func TestHeaderEncodeRLPNegativeBigInt(t *testing.T) {
	t.Parallel()

	header, extra := legacyHeader()
	header.Number = big.NewInt(-1)
	_, err := rlp.EncodeToBytes(WithHeader(header, extra))
	require.ErrorIs(t, err, rlp.ErrNegativeBigInt, "rlp.EncodeToBytes(header with negative Number)")
}

func TestHeaderDecodeJSONRequiredFields(t *testing.T) {
	t.Parallel()

	header, extra := legacyHeader()
	encoded, err := json.Marshal(WithHeader(header, extra))
	require.NoError(t, err, "json.Marshal(header)")

	for _, key := range []string{"miner", "extDataHash"} {
		for _, raw := range []json.RawMessage{nil, json.RawMessage("null")} {
			var m map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &m), "json.Unmarshal(header JSON)")
			if raw == nil {
				delete(m, key)
			} else {
				m[key] = raw
			}
			b, err := json.Marshal(m)
			require.NoError(t, err, "json.Marshal(map)")
			err = json.Unmarshal(b, new(types.Header))
			require.ErrorIsf(t, err, errMissingRequiredField, "json.Unmarshal(header JSON with %q = %s)", key, raw)
		}
	}
}

// TestHeaderJSONGolden pins [ethtypes.Header.MarshalJSON] to the shape coreth's
// generated codec produced, which newHeads subscribers receive: every unset
// field is present as null. withdrawalsRoot is the one key that codec did not
// emit; it comes from the upstream header and is always null on the C-Chain.
func TestHeaderJSONGolden(t *testing.T) {
	t.Parallel()

	header, extra := legacyHeader()
	got, err := json.Marshal(WithHeader(header, extra))
	require.NoError(t, err, "json.Marshal(legacy header)")
	require.JSONEq(t, legacyHeaderJSON, string(got), "legacy header JSON")
}

const legacyHeaderJSON = `{"baseFeePerGas": null, "blobGasUsed": null, "blockGasCost": null, "difficulty": "0x1", "excessBlobGas": null, "extDataGasUsed": null, "extDataHash": "0x1500000000000000000000000000000000000000000000000000000000000000", "extraData": "0x0d0e", "gasLimit": "0x7a1200", "gasUsed": "0x5208", "hash": "0x4da4a4a88e665d575a23776ce571d70be5100d2cfe2e574bea0c66c2f86964eb", "logsBloom": "0x07000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000", "minDelayExcess": null, "minPriceExponent": null, "miner": "0x0300000000000000000000000000000000000000", "mixHash": "0x0e00000000000000000000000000000000000000000000000000000000000000", "nonce": "0x0f00000000000000", "number": "0x9", "parentBeaconBlockRoot": null, "parentHash": "0x0100000000000000000000000000000000000000000000000000000000000000", "receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421", "settledExcess": null, "settledGasNumerator": null, "settledGasUnix": null, "settledHeight": null, "sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347", "stateRoot": "0x0400000000000000000000000000000000000000000000000000000000000000", "targetExponent": null, "timestamp": "0x5f5e1000", "timestampMilliseconds": null, "transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421", "withdrawalsRoot": null}`

func TestHeaderRLPFieldUnexpectedType(t *testing.T) {
	t.Parallel()

	p := new(int32)
	require.PanicsWithValue(t, "unexpected header field type *int32", func() {
		_ = writeRLPField(rlp.NewEncoderBuffer(nil), p)
	}, "writeRLPField(*int32)")
	require.PanicsWithValue(t, "unexpected header field type *int32", func() {
		_ = readRLPField(rlp.NewStream(nil, 0), p)
	}, "readRLPField(*int32)")
}
