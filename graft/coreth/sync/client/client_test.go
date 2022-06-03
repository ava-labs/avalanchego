// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
	clientstats "github.com/ava-labs/coreth/sync/client/stats"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const maxAttempts = 5

func TestGetCode(t *testing.T) {
	mockNetClient := &mockNetwork{}

	tests := map[string]struct {
		setupRequest func() (requestHashes []common.Hash, mockResponse message.CodeResponse, expectedCode [][]byte)
		expectedErr  error
	}{
		"normal": {
			setupRequest: func() ([]common.Hash, message.CodeResponse, [][]byte) {
				code := []byte("this is the code")
				codeHash := crypto.Keccak256Hash(code)
				codeSlices := [][]byte{code}
				return []common.Hash{codeHash}, message.CodeResponse{
					Data: codeSlices,
				}, codeSlices
			},
			expectedErr: nil,
		},
		"unexpected code bytes": {
			setupRequest: func() (requestHashes []common.Hash, mockResponse message.CodeResponse, expectedCode [][]byte) {
				return []common.Hash{{1}}, message.CodeResponse{
					Data: [][]byte{{1}},
				}, nil
			},
			expectedErr: errHashMismatch,
		},
		"too many code elements returned": {
			setupRequest: func() (requestHashes []common.Hash, mockResponse message.CodeResponse, expectedCode [][]byte) {
				return []common.Hash{{1}}, message.CodeResponse{
					Data: [][]byte{{1}, {2}},
				}, nil
			},
			expectedErr: errInvalidCodeResponseLen,
		},
		"too few code elements returned": {
			setupRequest: func() (requestHashes []common.Hash, mockResponse message.CodeResponse, expectedCode [][]byte) {
				return []common.Hash{{1}}, message.CodeResponse{
					Data: [][]byte{},
				}, nil
			},
			expectedErr: errInvalidCodeResponseLen,
		},
		"code size is too large": {
			setupRequest: func() (requestHashes []common.Hash, mockResponse message.CodeResponse, expectedCode [][]byte) {
				oversizedCode := make([]byte, params.MaxCodeSize+1)
				codeHash := crypto.Keccak256Hash(oversizedCode)
				return []common.Hash{codeHash}, message.CodeResponse{
					Data: [][]byte{oversizedCode},
				}, nil
			},
			expectedErr: errMaxCodeSizeExceeded,
		},
	}

	stateSyncClient := NewClient(&ClientConfig{
		NetworkClient:    mockNetClient,
		Codec:            message.Codec,
		Stats:            clientstats.NewNoOpStats(),
		MaxAttempts:      maxAttempts,
		MaxRetryDelay:    1,
		StateSyncNodeIDs: nil,
		BlockParser:      mockBlockParser,
	})

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			codeHashes, res, expectedCode := test.setupRequest()

			responseBytes, err := message.Codec.Marshal(message.Version, res)
			if err != nil {
				t.Fatal(err)
			}
			// Dirty hack required because the client will re-request if it encounters
			// an error.
			if test.expectedErr == nil {
				mockNetClient.mockResponse(1, responseBytes)
			} else {
				mockNetClient.mockResponse(maxAttempts, responseBytes)
			}

			codeBytes, err := stateSyncClient.GetCode(codeHashes)
			// If we expect an error, assert that one occurred and return
			if test.expectedErr != nil {
				assert.ErrorIs(t, err, test.expectedErr)
				assert.Equal(t, uint(maxAttempts), mockNetClient.numCalls)
				return
			}
			// Otherwise, assert there was no error and that the result is as expected
			assert.NoError(t, err)
			assert.Equal(t, len(codeBytes), len(expectedCode))
			for i, code := range codeBytes {
				assert.Equal(t, expectedCode[i], code)
			}
			assert.Equal(t, uint(1), mockNetClient.numCalls)
		})
	}
}

func TestGetBlocks(t *testing.T) {
	// set random seed for deterministic tests
	rand.Seed(1)

	var gspec = &core.Genesis{
		Config: params.TestChainConfig,
	}
	memdb := memorydb.New()
	genesis := gspec.MustCommit(memdb)
	engine := dummy.NewETHFaker()
	numBlocks := 110
	blocks, _, err := core.GenerateChain(params.TestChainConfig, genesis, engine, memdb, numBlocks, 0, func(i int, b *core.BlockGen) {})
	if err != nil {
		t.Fatal("unexpected error when generating test blockchain", err)
	}
	assert.Equal(t, numBlocks, len(blocks))

	// Construct client
	mockNetClient := &mockNetwork{}
	stateSyncClient := NewClient(&ClientConfig{
		NetworkClient:    mockNetClient,
		Codec:            message.Codec,
		Stats:            clientstats.NewNoOpStats(),
		MaxAttempts:      1,
		MaxRetryDelay:    1,
		StateSyncNodeIDs: nil,
		BlockParser:      mockBlockParser,
	})

	blocksRequestHandler := handlers.NewBlockRequestHandler(buildGetter(blocks), message.Codec, handlerstats.NewNoopHandlerStats())

	// encodeBlockSlice takes a slice of blocks that are ordered in increasing height order
	// and returns a slice of byte slices with those blocks encoded in reverse order
	encodeBlockSlice := func(blocks []*types.Block) [][]byte {
		blockBytes := make([][]byte, 0, len(blocks))
		for i := len(blocks) - 1; i >= 0; i-- {
			buf := new(bytes.Buffer)
			if err := blocks[i].EncodeRLP(buf); err != nil {
				t.Fatalf("failed to generate expected response %s", err)
			}
			blockBytes = append(blockBytes, buf.Bytes())
		}

		return blockBytes
	}
	tests := map[string]struct {
		request        message.BlockRequest
		getResponse    func(t *testing.T, request message.BlockRequest) []byte
		assertResponse func(t *testing.T, response []*types.Block)
		expectedErr    string
	}{
		"normal resonse": {
			request: message.BlockRequest{
				Hash:    blocks[100].Hash(),
				Height:  100,
				Parents: 16,
			},
			getResponse: func(t *testing.T, request message.BlockRequest) []byte {
				response, err := blocksRequestHandler.OnBlockRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal(err)
				}

				if len(response) == 0 {
					t.Fatal("Failed to generate valid response")
				}

				return response
			},
			assertResponse: func(t *testing.T, response []*types.Block) {
				assert.Equal(t, 16, len(response))
			},
		},
		"fewer than requested blocks": {
			request: message.BlockRequest{
				Hash:    blocks[100].Hash(),
				Height:  100,
				Parents: 16,
			},
			getResponse: func(t *testing.T, request message.BlockRequest) []byte {
				request.Parents -= 5
				response, err := blocksRequestHandler.OnBlockRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal(err)
				}

				if len(response) == 0 {
					t.Fatal("Failed to generate valid response")
				}

				return response
			},
			// If the server returns fewer than requested blocks, we should consider it valid
			assertResponse: func(t *testing.T, response []*types.Block) {
				assert.Equal(t, 11, len(response))
			},
		},
		"gibberish response": {
			request: message.BlockRequest{
				Hash:    blocks[100].Hash(),
				Height:  100,
				Parents: 16,
			},
			getResponse: func(t *testing.T, request message.BlockRequest) []byte {
				return []byte("gibberish")
			},
			expectedErr: errUnmarshalResponse.Error(),
		},
		"invalid value replacing block": {
			request: message.BlockRequest{
				Hash:    blocks[100].Hash(),
				Height:  100,
				Parents: 16,
			},
			getResponse: func(t *testing.T, request message.BlockRequest) []byte {
				response, err := blocksRequestHandler.OnBlockRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatalf("failed to get block response: %s", err)
				}
				var blockResponse message.BlockResponse
				if _, err = message.Codec.Unmarshal(response, &blockResponse); err != nil {
					t.Fatalf("failed to marshal block response: %s", err)
				}
				// Replace middle value with garbage data
				blockResponse.Blocks[10] = []byte("invalid value replacing block bytes")
				responseBytes, err := message.Codec.Marshal(message.Version, blockResponse)
				if err != nil {
					t.Fatalf("failed to marshal block response: %s", err)
				}

				return responseBytes
			},
			expectedErr: "failed to unmarshal response: rlp: expected input list for types.extblock",
		},
		"incorrect starting point": {
			request: message.BlockRequest{
				Hash:    blocks[100].Hash(),
				Height:  100,
				Parents: 16,
			},
			getResponse: func(t *testing.T, _ message.BlockRequest) []byte {
				response, err := blocksRequestHandler.OnBlockRequest(context.Background(), ids.GenerateTestNodeID(), 1, message.BlockRequest{
					Hash:    blocks[99].Hash(),
					Height:  99,
					Parents: 16,
				})
				if err != nil {
					t.Fatal(err)
				}

				if len(response) == 0 {
					t.Fatal("Failed to generate valid response")
				}

				return response
			},
			expectedErr: errHashMismatch.Error(),
		},
		"missing link in between blocks": {
			request: message.BlockRequest{
				Hash:    blocks[100].Hash(),
				Height:  100,
				Parents: 16,
			},
			getResponse: func(t *testing.T, request message.BlockRequest) []byte {
				// Encode blocks with a missing link
				blks := make([]*types.Block, 0)
				blks = append(blks, blocks[84:89]...)
				blks = append(blks, blocks[90:101]...)
				blockBytes := encodeBlockSlice(blks)

				blockResponse := message.BlockResponse{
					Blocks: blockBytes,
				}
				responseBytes, err := message.Codec.Marshal(message.Version, blockResponse)
				if err != nil {
					t.Fatalf("failed to marshal block response: %s", err)
				}

				return responseBytes
			},
			expectedErr: errHashMismatch.Error(),
		},
		"no blocks": {
			request: message.BlockRequest{
				Hash:    blocks[100].Hash(),
				Height:  100,
				Parents: 16,
			},
			getResponse: func(t *testing.T, request message.BlockRequest) []byte {
				blockResponse := message.BlockResponse{
					Blocks: nil,
				}
				responseBytes, err := message.Codec.Marshal(message.Version, blockResponse)
				if err != nil {
					t.Fatalf("failed to marshal block response: %s", err)
				}

				return responseBytes
			},
			expectedErr: errEmptyResponse.Error(),
		},
		"more than requested blocks": {
			request: message.BlockRequest{
				Hash:    blocks[100].Hash(),
				Height:  100,
				Parents: 16,
			},
			getResponse: func(t *testing.T, request message.BlockRequest) []byte {
				blockBytes := encodeBlockSlice(blocks[80:100])

				blockResponse := message.BlockResponse{
					Blocks: blockBytes,
				}
				responseBytes, err := message.Codec.Marshal(message.Version, blockResponse)
				if err != nil {
					t.Fatalf("failed to marshal block response: %s", err)
				}

				return responseBytes
			},
			expectedErr: errTooManyBlocks.Error(),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			responseBytes := test.getResponse(t, test.request)
			mockNetClient.mockResponse(1, responseBytes)

			blockResponse, err := stateSyncClient.GetBlocks(test.request.Hash, test.request.Height, test.request.Parents)
			if len(test.expectedErr) != 0 {
				if err == nil {
					t.Fatalf("Expected error: %s, but found no error", test.expectedErr)
				}
				assert.True(t, strings.Contains(err.Error(), test.expectedErr), "expected error to contain [%s], but found [%s]", test.expectedErr, err)
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			test.assertResponse(t, blockResponse)
		})
	}
}

func buildGetter(blocks []*types.Block) handlers.BlockProvider {
	return &handlers.TestBlockProvider{
		GetBlockFn: func(blockHash common.Hash, blockHeight uint64) *types.Block {
			requestedBlock := blocks[blockHeight]
			if requestedBlock.Hash() != blockHash {
				fmt.Printf("ERROR height=%d, hash=%s, parentHash=%s, reqHash=%s\n", blockHeight, blockHash, requestedBlock.ParentHash(), requestedBlock.Hash())
				return nil
			}
			return requestedBlock
		},
	}
}

func TestGetLeafs(t *testing.T) {
	rand.Seed(1)

	const leafsLimit = 1024

	trieDB := trie.NewDatabase(memorydb.New())
	largeTrieRoot, largeTrieKeys, _ := trie.GenerateTrie(t, trieDB, 100_000, common.HashLength)
	smallTrieRoot, _, _ := trie.GenerateTrie(t, trieDB, leafsLimit, common.HashLength)

	handler := handlers.NewLeafsRequestHandler(trieDB, nil, message.Codec, handlerstats.NewNoopHandlerStats())
	client := NewClient(&ClientConfig{
		NetworkClient:    &mockNetwork{},
		Codec:            message.Codec,
		Stats:            clientstats.NewNoOpStats(),
		MaxAttempts:      1,
		MaxRetryDelay:    1,
		StateSyncNodeIDs: nil,
		BlockParser:      mockBlockParser,
	})

	tests := map[string]struct {
		request        message.LeafsRequest
		getResponse    func(t *testing.T, request message.LeafsRequest) []byte
		assertResponse func(t *testing.T, response message.LeafsResponse)
		expectedErr    error
	}{
		"full response for small (single request) trie": {
			request: message.LeafsRequest{
				Root:     smallTrieRoot,
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}

				return response
			},
			assertResponse: func(t *testing.T, response message.LeafsResponse) {
				assert.False(t, response.More)
				assert.Equal(t, leafsLimit, len(response.Keys))
				assert.Equal(t, leafsLimit, len(response.Vals))
			},
		},
		"too many leaves in response": {
			request: message.LeafsRequest{
				Root:     smallTrieRoot,
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit / 2,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				modifiedRequest := request
				modifiedRequest.Limit = leafsLimit
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, modifiedRequest)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}

				return response
			},
			expectedErr: errTooManyLeaves,
		},
		"partial response to request for entire trie (full leaf limit)": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}

				return response
			},
			assertResponse: func(t *testing.T, response message.LeafsResponse) {
				assert.True(t, response.More)
				assert.Equal(t, leafsLimit, len(response.Keys))
				assert.Equal(t, leafsLimit, len(response.Vals))
			},
		},
		"partial response to request for middle range of trie (full leaf limit)": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    largeTrieKeys[1000],
				End:      largeTrieKeys[99000],
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}

				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}
				return response
			},
			assertResponse: func(t *testing.T, response message.LeafsResponse) {
				assert.True(t, response.More)
				assert.Equal(t, leafsLimit, len(response.Keys))
				assert.Equal(t, leafsLimit, len(response.Vals))
			},
		},
		"full response from near end of trie to end of trie (less than leaf limit)": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    largeTrieKeys[len(largeTrieKeys)-30], // Set start 30 keys from the end of the large trie
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}
				return response
			},
			assertResponse: func(t *testing.T, response message.LeafsResponse) {
				assert.False(t, response.More)
				assert.Equal(t, 30, len(response.Keys))
				assert.Equal(t, 30, len(response.Vals))
			},
		},
		"full response for intermediate range of trie (less than leaf limit)": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    largeTrieKeys[1000], // Set the range for 1000 leafs in an intermediate range of the trie
				End:      largeTrieKeys[1099], // (inclusive range)
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}

				return response
			},
			assertResponse: func(t *testing.T, response message.LeafsResponse) {
				assert.True(t, response.More)
				assert.Equal(t, 100, len(response.Keys))
				assert.Equal(t, 100, len(response.Vals))
			},
		},
		"removed first key in response": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}
				var leafResponse message.LeafsResponse
				if _, err := message.Codec.Unmarshal(response, &leafResponse); err != nil {
					t.Fatal(err)
				}
				leafResponse.Keys = leafResponse.Keys[1:]
				leafResponse.Vals = leafResponse.Vals[1:]

				modifiedResponse, err := message.Codec.Marshal(message.Version, leafResponse)
				if err != nil {
					t.Fatal(err)
				}
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"removed first key in response and replaced proof": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}
				var leafResponse message.LeafsResponse
				if _, err := message.Codec.Unmarshal(response, &leafResponse); err != nil {
					t.Fatal(err)
				}
				modifiedRequest := request
				modifiedRequest.Start = leafResponse.Keys[1]
				modifiedResponse, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 2, modifiedRequest)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"removed last key in response": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}
				var leafResponse message.LeafsResponse
				if _, err := message.Codec.Unmarshal(response, &leafResponse); err != nil {
					t.Fatal(err)
				}
				leafResponse.Keys = leafResponse.Keys[:len(leafResponse.Keys)-2]
				leafResponse.Vals = leafResponse.Vals[:len(leafResponse.Vals)-2]

				modifiedResponse, err := message.Codec.Marshal(message.Version, leafResponse)
				if err != nil {
					t.Fatal(err)
				}
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"removed key from middle of response": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}
				var leafResponse message.LeafsResponse
				if _, err := message.Codec.Unmarshal(response, &leafResponse); err != nil {
					t.Fatal(err)
				}
				// Remove middle key-value pair response
				leafResponse.Keys = append(leafResponse.Keys[:100], leafResponse.Keys[101:]...)
				leafResponse.Vals = append(leafResponse.Vals[:100], leafResponse.Vals[101:]...)

				modifiedResponse, err := message.Codec.Marshal(message.Version, leafResponse)
				if err != nil {
					t.Fatal(err)
				}
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"corrupted value in middle of response": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}
				var leafResponse message.LeafsResponse
				if _, err := message.Codec.Unmarshal(response, &leafResponse); err != nil {
					t.Fatal(err)
				}
				// Remove middle key-value pair response
				leafResponse.Vals[100] = []byte("garbage value data")

				modifiedResponse, err := message.Codec.Marshal(message.Version, leafResponse)
				if err != nil {
					t.Fatal(err)
				}
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"all proof keys removed from response": {
			request: message.LeafsRequest{
				Root:     largeTrieRoot,
				Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
				End:      bytes.Repeat([]byte{0xff}, common.HashLength),
				Limit:    leafsLimit,
				NodeType: message.StateTrieNode,
			},
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
				if err != nil {
					t.Fatal("unexpected error in calling leafs request handler", err)
				}
				if len(response) == 0 {
					t.Fatal("Failed to create valid response")
				}

				var leafResponse message.LeafsResponse
				if _, err := message.Codec.Unmarshal(response, &leafResponse); err != nil {
					t.Fatal(err)
				}
				// Remove the proof keys
				leafResponse.ProofKeys = nil
				leafResponse.ProofVals = nil

				modifiedResponse, err := message.Codec.Marshal(message.Version, leafResponse)
				if err != nil {
					t.Fatal(err)
				}
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			responseBytes := test.getResponse(t, test.request)

			response, _, err := parseLeafsResponse(client.codec, test.request, responseBytes)
			if test.expectedErr != nil {
				if err == nil {
					t.Fatalf("Expected error: %s, but found no error", test.expectedErr)
				}
				assert.True(t, strings.Contains(err.Error(), test.expectedErr.Error()))
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			leafsResponse, ok := response.(message.LeafsResponse)
			if !ok {
				t.Fatalf("parseLeafsResponse returned incorrect type %T", response)
			}
			test.assertResponse(t, leafsResponse)
		})
	}
}

func TestGetLeafsRetries(t *testing.T) {
	rand.Seed(1)

	trieDB := trie.NewDatabase(memorydb.New())
	root, _, _ := trie.GenerateTrie(t, trieDB, 100_000, common.HashLength)

	handler := handlers.NewLeafsRequestHandler(trieDB, nil, message.Codec, handlerstats.NewNoopHandlerStats())
	mockNetClient := &mockNetwork{}

	const maxAttempts = 8
	client := NewClient(&ClientConfig{
		NetworkClient:    mockNetClient,
		Codec:            message.Codec,
		Stats:            clientstats.NewNoOpStats(),
		MaxAttempts:      maxAttempts,
		MaxRetryDelay:    1,
		StateSyncNodeIDs: nil,
		BlockParser:      mockBlockParser,
	})

	request := message.LeafsRequest{
		Root:     root,
		Start:    bytes.Repeat([]byte{0x00}, common.HashLength),
		End:      bytes.Repeat([]byte{0xff}, common.HashLength),
		Limit:    defaultLeafRequestLimit,
		NodeType: message.StateTrieNode,
	}
	goodResponse, responseErr := handler.OnLeafsRequest(context.Background(), ids.GenerateTestNodeID(), 1, request)
	assert.NoError(t, responseErr)
	mockNetClient.mockResponse(1, goodResponse)

	res, err := client.GetLeafs(request)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 1024, len(res.Keys))
	assert.Equal(t, 1024, len(res.Vals))

	// Succeeds within the allotted number of attempts
	invalidResponse := []byte("invalid response")
	mockNetClient.mockResponses(invalidResponse, invalidResponse, goodResponse)

	res, err = client.GetLeafs(request)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 1024, len(res.Keys))
	assert.Equal(t, 1024, len(res.Vals))

	// Test that we hit the retry limit
	mockNetClient.mockResponse(maxAttempts, invalidResponse)
	_, err = client.GetLeafs(request)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), errExceededRetryLimit.Error()))
}

func TestStateSyncNodes(t *testing.T) {
	mockNetClient := &mockNetwork{}

	stateSyncNodes := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}
	client := NewClient(&ClientConfig{
		NetworkClient:    mockNetClient,
		Codec:            message.Codec,
		Stats:            clientstats.NewNoOpStats(),
		MaxAttempts:      4,
		MaxRetryDelay:    1,
		StateSyncNodeIDs: stateSyncNodes,
		BlockParser:      mockBlockParser,
	})
	mockNetClient.response = [][]byte{{1}, {2}, {3}, {4}}

	// send some request, doesn't matter what it is because we're testing the interaction with state sync nodes here
	response, err := client.GetLeafs(message.LeafsRequest{})
	assert.Error(t, err)
	assert.Empty(t, response)

	// assert all nodes were called
	assert.Contains(t, mockNetClient.nodesRequested, stateSyncNodes[0])
	assert.Contains(t, mockNetClient.nodesRequested, stateSyncNodes[1])
	assert.Contains(t, mockNetClient.nodesRequested, stateSyncNodes[2])
	assert.Contains(t, mockNetClient.nodesRequested, stateSyncNodes[3])
}
