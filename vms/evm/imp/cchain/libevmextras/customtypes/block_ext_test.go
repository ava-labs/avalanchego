// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestCopyHeader(t *testing.T) {
	t.Parallel()

	copiedEmpty := func() *types.Header {
		return &types.Header{Difficulty: new(big.Int), Number: new(big.Int)}
	}
	tests := []struct {
		name         string
		header, want *types.Header
	}{
		{
			name:   "no_extra_attached",
			header: &types.Header{},
			want:   WithHeaderExtra(copiedEmpty(), &HeaderExtra{}),
		},
		{
			name:   "nil_extra",
			header: WithHeaderExtra(&types.Header{}, nil),
			want:   WithHeaderExtra(copiedEmpty(), nil),
		},
		{
			name:   "empty_extra",
			header: WithHeaderExtra(&types.Header{}, &HeaderExtra{}),
			want:   WithHeaderExtra(copiedEmpty(), &HeaderExtra{}),
		},
		{
			name:   "filled",
			header: utils.First(headerWithNonZeroFields()),
			want:   utils.First(headerWithNonZeroFields()),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := types.CopyHeader(test.header)
			require.Equal(t, test.want, got, "types.CopyHeader()")
			require.Equal(t, GetHeaderExtra(test.want), GetHeaderExtra(got), "GetHeaderExtra(types.CopyHeader())")
		})
	}
}

// TestCopyHeaderDeepCopiesExtra checks that [HeaderExtra.PostCopy] clones
// every pointer field, which require.Equal can't tell from aliasing.
func TestCopyHeaderDeepCopiesExtra(t *testing.T) {
	t.Parallel()

	header, extra := headerWithNonZeroFields()
	src := reflect.ValueOf(extra).Elem()
	got := reflect.ValueOf(GetHeaderExtra(types.CopyHeader(header))).Elem()
	for i := range src.NumField() {
		field := src.Type().Field(i)
		if !field.IsExported() || field.Type.Kind() != reflect.Pointer {
			continue
		}
		require.False(t, src.Field(i).IsNil(), "headerWithNonZeroFields() leaves %s nil", field.Name)
		require.NotEqual(t, src.Field(i).UnsafePointer(), got.Field(i).UnsafePointer(), "%s shares memory with the original", field.Name)
	}
}

// blockWithNonZeroFields returns a block carrying a transaction, an uncle,
// withdrawals and a [BlockBodyExtra] with every field set.
func blockWithNonZeroFields() (*types.Block, *BlockBodyExtra) {
	header := WithHeaderExtra(
		&types.Header{
			ParentHash: common.Hash{1},
		},
		&HeaderExtra{
			ExtDataHash: common.Hash{2},
		},
	)

	tx := types.NewTransaction(1, common.Address{2}, big.NewInt(3), 4, big.NewInt(5), []byte{6})
	txs := []*types.Transaction{tx}

	uncle := WithHeaderExtra(
		&types.Header{
			Difficulty: big.NewInt(7),
			Number:     big.NewInt(8),
			ParentHash: common.Hash{9},
		},
		&HeaderExtra{
			ExtDataHash: common.Hash{10},
		},
	)
	uncles := []*types.Header{uncle}

	receipts := []*types.Receipt{{PostState: []byte{11}}}

	block := types.NewBlock(header, txs, uncles, receipts, trie.NewStackTrie(nil))
	withdrawals := []*types.Withdrawal{{Index: 12}}
	block = block.WithWithdrawals(withdrawals)
	extra := &BlockBodyExtra{
		Version: 13,
		ExtData: &[]byte{14},
	}
	return WithBlockExtra(block, extra), extra
}

func TestBlockExtraRLP(t *testing.T) {
	t.Parallel()

	block, extra := blockWithNonZeroFields()
	allFieldsSet(t, extra)
	encoded, err := rlp.EncodeToBytes(block)
	require.NoError(t, err, "rlp.EncodeToBytes(block)")

	// Golden data from coreth's codec. WARNING: changing these values can break
	// backwards compatibility with extreme consequences.
	const wantHex = "f90446f9020ea00100000000000000000000000000000000000000000000000000000000000000a008539331084089cedbaf7771d0f5f69847f246e0676e4d96091a49c53c89360b940000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000a04863dd4b78c4e439c248378fe140f4101d3c00a4efa87e474de54245f2aa14fda0be18e0b68619263d822ea3476f100a4eaa0bd7da2632b102a67be20b953fe494b9010000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000808080808080a00000000000000000000000000000000000000000000000000000000000000000880000000000000000a00200000000000000000000000000000000000000000000000000000000000000dedd0105049402000000000000000000000000000000000000000306808080f90211f9020ea00900000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000940000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000b9010000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000070880808080a00000000000000000000000000000000000000000000000000000000000000000880000000000000000a00a000000000000000000000000000000000000000000000000000000000000000d0e"

	require.Equal(t, wantHex, hex.EncodeToString(encoded), "golden data")

	decoded := new(types.Block)
	require.NoError(t, rlp.DecodeBytes(encoded, decoded), "rlp.DecodeBytes(block RLP)")
	require.Equal(t, block.Hash(), decoded.Hash(), "decoded Block.Hash()")
	require.Equal(t, extra, GetBlockExtra(decoded), "decoded BlockBodyExtra")
}

// TestBlockBodyExtraCopiedWithBlock checks that libevm deep-copies the extra
// when it derives a new block, via [BlockBodyExtra.Copy].
func TestBlockBodyExtraCopiedWithBlock(t *testing.T) {
	t.Parallel()

	block, extra := blockWithNonZeroFields()
	got := GetBlockExtra(block.WithBody(*block.Body()))
	require.Equal(t, extra, got, "GetBlockExtra(Block.WithBody())")
	(*got.ExtData)[0]++
	require.Equal(t, []byte{14}, *extra.ExtData, "Block.WithBody() shares ExtData with the original")
}

func TestBlockGetters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		blockExtra  *BlockBodyExtra
		wantVersion uint32
		wantExtData []byte
	}{
		{
			name:       "empty",
			blockExtra: &BlockBodyExtra{},
		},
		{
			name: "fields_set",
			blockExtra: &BlockBodyExtra{
				Version: 3,
				ExtData: &[]byte{4},
			},
			wantVersion: 3,
			wantExtData: []byte{4},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			block := types.NewBlock(WithHeaderExtra(&types.Header{}, &HeaderExtra{}), nil, nil, nil, trie.NewStackTrie(nil))
			block = WithBlockExtra(block, test.blockExtra)

			require.Equal(t, test.wantExtData, BlockExtData(block), "BlockExtData()")
			require.Equal(t, test.wantVersion, BlockVersion(block), "BlockVersion()")
		})
	}
}

func TestCalcExtDataHash(t *testing.T) {
	t.Parallel()

	// Golden: the hash every C-Chain block without atomic transactions commits
	// to. nil and empty ext data MUST hash identically.
	want := common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	require.Equal(t, want, EmptyExtDataHash, "EmptyExtDataHash")
	require.Equal(t, want, CalcExtDataHash(nil), "CalcExtDataHash(nil)")
	require.Equal(t, want, CalcExtDataHash([]byte{}), "CalcExtDataHash(empty)")
	require.Equal(t, types.RLPHash([]byte{1}), CalcExtDataHash([]byte{1}), "CalcExtDataHash(non-empty)")
}

func TestNewBlock(t *testing.T) {
	t.Parallel()

	// This transaction is generated beforehand because of its unexported time field being set
	// on creation.
	testTx := types.NewTransaction(0, common.Address{1}, big.NewInt(2), 3, big.NewInt(4), []byte{5})

	tests := []struct {
		name      string
		header    *types.Header
		txs       []*types.Transaction
		receipts  []*types.Receipt
		extdata   []byte
		wantBlock func() *types.Block
	}{
		{
			name:   "empty",
			header: WithHeaderExtra(&types.Header{}, &HeaderExtra{}),
			wantBlock: func() *types.Block {
				header := WithHeaderExtra(&types.Header{}, &HeaderExtra{ExtDataHash: EmptyExtDataHash})
				block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
				return WithBlockExtra(block, &BlockBodyExtra{ExtData: new([]byte)})
			},
		},
		{
			name:   "header_nil_extra",
			header: &types.Header{},
			wantBlock: func() *types.Block {
				header := WithHeaderExtra(&types.Header{}, &HeaderExtra{ExtDataHash: EmptyExtDataHash})
				block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
				return WithBlockExtra(block, &BlockBodyExtra{ExtData: new([]byte)})
			},
		},
		{
			name: "ext_data_hash_recalculated",
			header: WithHeaderExtra(
				&types.Header{},
				&HeaderExtra{
					ExtDataHash: common.Hash{1}, // must be overwritten
				},
			),
			extdata: []byte{2},
			wantBlock: func() *types.Block {
				header := WithHeaderExtra(
					&types.Header{},
					&HeaderExtra{ExtDataHash: CalcExtDataHash([]byte{2})},
				)
				block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
				return WithBlockExtra(block, &BlockBodyExtra{ExtData: &[]byte{2}})
			},
		},
		{
			name: "filled",
			header: WithHeaderExtra(
				&types.Header{GasLimit: 1},
				&HeaderExtra{
					ExtDataGasUsed: big.NewInt(3),
					BlockGasCost:   big.NewInt(4),
				},
			),
			txs:      []*types.Transaction{testTx},
			receipts: []*types.Receipt{{PostState: []byte{7}}},
			extdata:  []byte{8},
			wantBlock: func() *types.Block {
				header := WithHeaderExtra(
					&types.Header{GasLimit: 1},
					&HeaderExtra{
						ExtDataHash:    CalcExtDataHash([]byte{8}),
						ExtDataGasUsed: big.NewInt(3),
						BlockGasCost:   big.NewInt(4),
					},
				)
				block := types.NewBlock(header, []*types.Transaction{testTx}, nil, []*types.Receipt{{PostState: []byte{7}}}, trie.NewStackTrie(nil))
				return WithBlockExtra(block, &BlockBodyExtra{ExtData: &[]byte{8}})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			block := NewBlock(test.header, test.txs, test.receipts, test.extdata)
			require.Equal(t, test.wantBlock(), block, "NewBlock()")
		})
	}
}

// TestBlockRPCJSONRoundTrip goes through [types.Block.PostRPCMarshal]
// and [types.Block.PostRPCUnmarshal], which are what ethapi and ethclient call.
func TestBlockRPCJSONRoundTrip(t *testing.T) {
	t.Parallel()

	newBlock := func() *types.Block {
		return types.NewBlock(WithHeaderExtra(&types.Header{}, &HeaderExtra{}), nil, nil, nil, trie.NewStackTrie(nil))
	}
	tests := []struct {
		name    string
		extData *[]byte
		want    *[]byte
	}{
		{
			// Nil ext data is marshalled as "0x", so it round-trips as empty.
			name: "nil",
			want: &[]byte{},
		},
		{
			name:    "empty",
			extData: &[]byte{},
			want:    &[]byte{},
		},
		{
			name:    "set",
			extData: &[]byte{1, 2, 3},
			want:    &[]byte{1, 2, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := WithBlockExtra(newBlock(), &BlockBodyExtra{ExtData: tt.extData})
			marshalled := map[string]any{}
			in.PostRPCMarshal(marshalled)
			raw, err := json.Marshal(marshalled)
			require.NoError(t, err, "json.Marshal(Block.PostRPCMarshal())")

			got := newBlock()
			require.NoError(t, got.PostRPCUnmarshal(raw), "Block.PostRPCUnmarshal(%s)", raw)
			require.Equal(t, tt.want, GetBlockExtra(got).ExtData, "GetBlockExtra(Block.PostRPCUnmarshal(%s)).ExtData", raw)
		})
	}

	err := newBlock().PostRPCUnmarshal([]byte(`{"blockExtraData":"zz"}`))
	require.ErrorAs(t, err, new(*json.UnmarshalTypeError), "Block.PostRPCUnmarshal(bad hex)")
}

// TestBlockRLPGolden pins the encodings of historical C-Chain block shapes.
func TestBlockRLPGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		encoded            string
		wantHash           common.Hash
		wantExtDataHash    common.Hash
		wantExtDataGasUsed *big.Int
		wantBlockGasCost   *big.Int
	}{
		{
			name:            "legacy",
			encoded:         "f90291f90217a04504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902a01dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347940100000000000000000000000000000000000000a00202e12a30c13562445052414c24dce5f1c530bb164e2a50897f0a6a1f78f158a0ecdf3b2c973d4156782b95816451fe9ed66b099cdca22f1168591ae2087765f4a0056b23fbba480696b65fe5a59b8f2148a1299103c4f57df839233af2cf4ca2d2b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000103837a12008252088460674e8a80a00000000000000000000000000000000000000000000000000000000000000000880000000000000000a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421f872f870808534630b8a00825208941954b772512974978793a809ecd8dce02dc71ba989014d1120d7b160000080830150f4a0beebf298ec38f9f4204f924686c4e5dd00f525fc1979ad224661ed2839ed55fda0267c480d1236c1684bdbad564a422e0a05007d7e8ca1acefe34e790b1d3a450ec08080",
			wantHash:        common.HexToHash("0x0608e5d5e13c337f226b621a0b08b3d50470f1961329826fd59f5a241d1df49e"),
			wantExtDataHash: EmptyExtDataHash,
		},
		{
			name:            "ext_data",
			encoded:         "f903f2f90215a02a0d1d68d26eb213cf1c6c1e6abbaf374f0ee9a5428558df334c36d380c6a080a01dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347940100000000000000000000000000000000000000a0c0caa90fe3722cb2e288f7998d54a855a6d40f67e0e77a695d0d65dad22c6290a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000102837a1200808460674e3380a00000000000000000000000000000000000000000000000000000000000000000880000000000000000a0296ff3bfdebf7c4b1fb71f589d69ed03b1c59b278d1780d54dc86ea7cb87cf17c0c080b901d400000000000000003039c85fc1980a77c5da78fe5486233fc09a769bb812bcb2cc548cf9495d046b3f1bd891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf000000028a0f7c3e4d840143671a4c4ecacccb4d60fb97dce97a7aa5d60dfd072a7509cf00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500002d79883d20000000000100000000e0d5c4edc78f594b79025a56c44933c28e8ba3e51e6e23318727eeaac10eb27d00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500002d79883d20000000000100000000000000016dc8ea73dd39ab12fa2ecbc3427abaeb87d56fd800005af3107a4000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000200000009000000010d9f115cd63c3ab78b5b82cfbe4339cd6be87f21cda14cf192b269c7a6cb2d03666aa8f8b23ca0a2ceee4050e75c9b05525a17aa1dd0e9ea391a185ce395943f0000000009000000010d9f115cd63c3ab78b5b82cfbe4339cd6be87f21cda14cf192b269c7a6cb2d03666aa8f8b23ca0a2ceee4050e75c9b05525a17aa1dd0e9ea391a185ce395943f00",
			wantHash:        common.HexToHash("0x4504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902"),
			wantExtDataHash: common.HexToHash("0x296ff3bfdebf7c4b1fb71f589d69ed03b1c59b278d1780d54dc86ea7cb87cf17"),
		},
		{
			name:               "ap4",
			encoded:            "f90335f90226a04504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902a00000000000000000000000000000000000000000000000000000000000000000948888f1f195afa192cfee860698584c030f4c9db1a0ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000008302000003832fefd8825208845506eb0780a0bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff49888a13a5a8c8f2bb1c4a00000000000000000000000000000000000000000000000000000000000000000843b9aca008261a8830f4240f90106f85f800a82c35094095e7baea6a6c7c4c2dfeb977efac326af552d870a801ba09bea4c4daac7c7c52e093e6a4c35dbbcf8856f1af7b059ba20253e70848d094fa08a8fae537ce25ed8cb5af9adac3f141af69bd515bd2ba031522df09b97dd72b1b8a302f8a0018080843b9aca008301e24194095e7baea6a6c7c4c2dfeb977efac326af552d878080f838f7940000000000000000000000000000000000000001e1a0000000000000000000000000000000000000000000000000000000000000000080a0fe38ca4e44a30002ac54af7cf922a6ac2ba11b7d22f548e8ecb3f51f41cb31b0a06de6a5cbae13c0c856e33acf021b51819636cfc009d39eafb9f606d546e305a8c08080",
			wantHash:           common.HexToHash("0xc41340f5d2af79a12373bc8d6f0f05f9f98b240834608f428da171449e8a1468"),
			wantExtDataGasUsed: big.NewInt(25_000),
			wantBlockGasCost:   big.NewInt(1_000_000),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded := common.FromHex(tt.encoded)
			block := new(types.Block)
			require.NoError(t, rlp.DecodeBytes(encoded, block), "rlp.DecodeBytes(block)")
			require.Equal(t, tt.wantHash, block.Hash(), "Block.Hash()")

			extra := GetHeaderExtra(block.Header())
			require.Equal(t, tt.wantExtDataHash, extra.ExtDataHash, "ExtDataHash")
			require.Equal(t, tt.wantExtDataGasUsed, extra.ExtDataGasUsed, "ExtDataGasUsed")
			require.Equal(t, tt.wantBlockGasCost, extra.BlockGasCost, "BlockGasCost")

			got, err := rlp.EncodeToBytes(block)
			require.NoError(t, err, "rlp.EncodeToBytes(block)")
			require.Equal(t, encoded, got, "re-encoded block")
		})
	}
}
