// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/ids"

	clientstats "github.com/ava-labs/avalanchego/graft/coreth/sync/client/stats"
	handlerstats "github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

func TestGetCode(t *testing.T) {
	tests := map[string]struct {
		setupRequest func() (requestHashes []common.Hash, testResponse message.CodeResponse, expectedCode [][]byte)
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
			setupRequest: func() (requestHashes []common.Hash, testResponse message.CodeResponse, expectedCode [][]byte) {
				return []common.Hash{{1}}, message.CodeResponse{
					Data: [][]byte{{1}},
				}, nil
			},
			expectedErr: errHashMismatch,
		},
		"too many code elements returned": {
			setupRequest: func() (requestHashes []common.Hash, testResponse message.CodeResponse, expectedCode [][]byte) {
				return []common.Hash{{1}}, message.CodeResponse{
					Data: [][]byte{{1}, {2}},
				}, nil
			},
			expectedErr: errInvalidCodeResponseLen,
		},
		"too few code elements returned": {
			setupRequest: func() (requestHashes []common.Hash, testResponse message.CodeResponse, expectedCode [][]byte) {
				return []common.Hash{{1}}, message.CodeResponse{
					Data: [][]byte{},
				}, nil
			},
			expectedErr: errInvalidCodeResponseLen,
		},
		"code size is too large": {
			setupRequest: func() (requestHashes []common.Hash, testResponse message.CodeResponse, expectedCode [][]byte) {
				oversizedCode := make([]byte, ethparams.MaxCodeSize+1)
				codeHash := crypto.Keccak256Hash(oversizedCode)
				return []common.Hash{codeHash}, message.CodeResponse{
					Data: [][]byte{oversizedCode},
				}, nil
			},
			expectedErr: errMaxCodeSizeExceeded,
		},
	}

	utilstest.ForEachCodec(t, func(_ string, c codec.Manager) {
		for name, test := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				testNetClient := &testNetwork{}
				stateSyncClient := New(&Config{
					NetworkClient:    testNetClient,
					Codec:            c,
					Stats:            clientstats.NewNoOpStats(),
					StateSyncNodeIDs: nil,
					BlockParser:      newTestBlockParser(),
				})
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				codeHashes, res, expectedCode := test.setupRequest()

				responseBytes, err := c.Marshal(message.Version, res)
				require.NoError(t, err)
				// Dirty hack required because the client will re-request if it encounters
				// an error.
				attempted := false
				if test.expectedErr == nil {
					testNetClient.testResponse(1, nil, responseBytes)
				} else {
					testNetClient.testResponse(2, func() {
						// Cancel before the second attempt is processed.
						if attempted {
							cancel()
						}
						attempted = true
					}, responseBytes)
				}

				codeBytes, err := stateSyncClient.GetCode(ctx, codeHashes)
				require.ErrorIs(t, err, test.expectedErr)
				// If we expected an error, verify retry behavior and return
				if test.expectedErr != nil {
					require.Equal(t, uint(2), testNetClient.numCalls)
					return
				}
				// Otherwise, require that the result is as expected
				require.Len(t, codeBytes, len(expectedCode))
				for i, code := range codeBytes {
					require.Equal(t, expectedCode[i], code)
				}
				require.Equal(t, uint(1), testNetClient.numCalls)
			})
		}
	})
}

func TestGetBlocks(t *testing.T) {
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
	}
	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, nil)
	genesis := gspec.MustCommit(memdb, tdb)
	engine := dummy.NewETHFaker()
	numBlocks := 110
	blocks, _, err := core.GenerateChain(params.TestChainConfig, genesis, engine, memdb, numBlocks, 0, func(_ int, _ *core.BlockGen) {})
	require.NoError(t, err)
	require.Len(t, blocks, numBlocks)

	utilstest.ForEachCodec(t, func(_ string, c codec.Manager) {
		blocksRequestHandler := handlers.NewBlockRequestHandler(buildGetter(blocks), c, handlerstats.NewNoopHandlerStats())

		// encodeBlockSlice takes a slice of blocks that are ordered in increasing height order
		// and returns a slice of byte slices with those blocks encoded in reverse order
		encodeBlockSlice := func(blocks []*types.Block) [][]byte {
			blockBytes := make([][]byte, 0, len(blocks))
			for i := len(blocks) - 1; i >= 0; i-- {
				buf := new(bytes.Buffer)
				require.NoError(t, blocks[i].EncodeRLP(buf))
				blockBytes = append(blockBytes, buf.Bytes())
			}

			return blockBytes
		}
		tests := map[string]struct {
			request        message.BlockRequest
			getResponse    func(t *testing.T, request message.BlockRequest) []byte
			assertResponse func(t *testing.T, response []*types.Block)
			expectedErr    error
		}{
			"normal resonse": {
				request: message.BlockRequest{
					Hash:    blocks[100].Hash(),
					Height:  100,
					Parents: 16,
				},
				getResponse: func(t *testing.T, request message.BlockRequest) []byte {
					response, err := blocksRequestHandler.OnBlockRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
					require.NoError(t, err)
					require.NotEmpty(t, response, "Failed to generate valid response")

					return response
				},
				assertResponse: func(t *testing.T, response []*types.Block) {
					require.Len(t, response, 16)
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
					response, err := blocksRequestHandler.OnBlockRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
					require.NoError(t, err)
					require.NotEmpty(t, response)

					return response
				},
				// If the server returns fewer than requested blocks, we should consider it valid
				assertResponse: func(t *testing.T, response []*types.Block) {
					require.Len(t, response, 11)
				},
			},
			"gibberish response": {
				request: message.BlockRequest{
					Hash:    blocks[100].Hash(),
					Height:  100,
					Parents: 16,
				},
				getResponse: func(_ *testing.T, _ message.BlockRequest) []byte {
					return []byte("gibberish")
				},
				expectedErr: errUnmarshalResponse,
			},
			"invalid value replacing block": {
				request: message.BlockRequest{
					Hash:    blocks[100].Hash(),
					Height:  100,
					Parents: 16,
				},
				getResponse: func(t *testing.T, request message.BlockRequest) []byte {
					response, err := blocksRequestHandler.OnBlockRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
					require.NoError(t, err)
					var blockResponse message.BlockResponse
					_, err = c.Unmarshal(response, &blockResponse)
					require.NoError(t, err)
					// Replace middle value with garbage data
					blockResponse.Blocks[10] = []byte("invalid value replacing block bytes")
					responseBytes, err := c.Marshal(message.Version, blockResponse)
					require.NoError(t, err)

					return responseBytes
				},
				expectedErr: errUnmarshalResponse,
			},
			"incorrect starting point": {
				request: message.BlockRequest{
					Hash:    blocks[100].Hash(),
					Height:  100,
					Parents: 16,
				},
				getResponse: func(t *testing.T, _ message.BlockRequest) []byte {
					response, err := blocksRequestHandler.OnBlockRequest(t.Context(), ids.GenerateTestNodeID(), 1, message.BlockRequest{
						Hash:    blocks[99].Hash(),
						Height:  99,
						Parents: 16,
					})
					require.NoError(t, err)
					require.NotEmpty(t, response)

					return response
				},
				expectedErr: errHashMismatch,
			},
			"missing link in between blocks": {
				request: message.BlockRequest{
					Hash:    blocks[100].Hash(),
					Height:  100,
					Parents: 16,
				},
				getResponse: func(t *testing.T, _ message.BlockRequest) []byte {
					// Encode blocks with a missing link
					blks := make([]*types.Block, 0)
					blks = append(blks, blocks[84:89]...)
					blks = append(blks, blocks[90:101]...)
					blockBytes := encodeBlockSlice(blks)

					blockResponse := message.BlockResponse{
						Blocks: blockBytes,
					}
					responseBytes, err := c.Marshal(message.Version, blockResponse)
					require.NoError(t, err)

					return responseBytes
				},
				expectedErr: errHashMismatch,
			},
			"no blocks": {
				request: message.BlockRequest{
					Hash:    blocks[100].Hash(),
					Height:  100,
					Parents: 16,
				},
				getResponse: func(t *testing.T, _ message.BlockRequest) []byte {
					blockResponse := message.BlockResponse{
						Blocks: nil,
					}
					responseBytes, err := c.Marshal(message.Version, blockResponse)
					require.NoError(t, err)

					return responseBytes
				},
				expectedErr: errEmptyResponse,
			},
			"more than requested blocks": {
				request: message.BlockRequest{
					Hash:    blocks[100].Hash(),
					Height:  100,
					Parents: 16,
				},
				getResponse: func(t *testing.T, _ message.BlockRequest) []byte {
					blockBytes := encodeBlockSlice(blocks[80:100])

					blockResponse := message.BlockResponse{
						Blocks: blockBytes,
					}
					responseBytes, err := c.Marshal(message.Version, blockResponse)
					require.NoError(t, err)

					return responseBytes
				},
				expectedErr: errTooManyBlocks,
			},
		}
		for name, test := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				// Construct client
				testNetClient := &testNetwork{}
				stateSyncClient := New(&Config{
					NetworkClient:    testNetClient,
					Codec:            c,
					Stats:            clientstats.NewNoOpStats(),
					StateSyncNodeIDs: nil,
					BlockParser:      newTestBlockParser(),
				})

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				responseBytes := test.getResponse(t, test.request)
				if test.expectedErr == nil {
					testNetClient.testResponse(1, nil, responseBytes)
				} else {
					attempted := false
					testNetClient.testResponse(2, func() {
						if attempted {
							cancel()
						}
						attempted = true
					}, responseBytes)
				}

				blockResponse, err := stateSyncClient.GetBlocks(ctx, test.request.Hash, test.request.Height, test.request.Parents)
				require.ErrorIs(t, err, test.expectedErr)
				if test.expectedErr != nil {
					return
				}

				test.assertResponse(t, blockResponse)
			})
		}
	})
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
	const leafsLimit = 1024
	r := rand.New(rand.NewSource(1))

	trieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	largeTrieRoot, largeTrieKeys, _ := synctest.GenerateIndependentTrie(t, r, trieDB, 100_000, common.HashLength)
	smallTrieRoot, _, _ := synctest.GenerateIndependentTrie(t, r, trieDB, leafsLimit, common.HashLength)

	handler := handlers.NewLeafsRequestHandler(
		trieDB,
		message.StateTrieKeyLength,
		nil,
		message.CorethCodec,
		handlerstats.NewNoopHandlerStats(),
	)

	tests := map[string]struct {
		request         message.LeafsRequest
		getResponse     func(t *testing.T, request message.LeafsRequest) []byte
		requireResponse func(t *testing.T, response message.LeafsResponse)
		expectedErr     error
	}{
		"full response for small (single request) trie": {
			request: newLeafsRequest(
				smallTrieRoot,
				common.Hash{},
				bytes.Repeat([]byte{0x00}, common.HashLength),
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)

				return response
			},
			requireResponse: func(t *testing.T, response message.LeafsResponse) {
				require.False(t, response.More)
				require.Len(t, response.Keys, leafsLimit)
				require.Len(t, response.Vals, leafsLimit)
			},
		},
		"too many leaves in response": {
			request: newLeafsRequest(
				smallTrieRoot,
				common.Hash{},
				bytes.Repeat([]byte{0x00}, common.HashLength),
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit/2,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				modifiedRequest := newLeafsRequest(
					request.RootHash(),
					request.AccountHash(),
					request.StartKey(),
					request.EndKey(),
					leafsLimit,
					request.NodeTypeValue(),
				)
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, modifiedRequest)
				require.NoError(t, err)
				require.NotEmpty(t, response)

				return response
			},
			expectedErr: errTooManyLeaves,
		},
		"partial response to request for entire trie (full leaf limit)": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				bytes.Repeat([]byte{0x00}, common.HashLength),
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)

				return response
			},
			requireResponse: func(t *testing.T, response message.LeafsResponse) {
				require.True(t, response.More)
				require.Len(t, response.Keys, leafsLimit)
				require.Len(t, response.Vals, leafsLimit)
			},
		},
		"partial response to request for middle range of trie (full leaf limit)": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				largeTrieKeys[1000],
				largeTrieKeys[99000],
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)

				return response
			},
			requireResponse: func(t *testing.T, response message.LeafsResponse) {
				require.True(t, response.More)
				require.Len(t, response.Keys, leafsLimit)
				require.Len(t, response.Vals, leafsLimit)
			},
		},
		"full response from near end of trie to end of trie (less than leaf limit)": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				largeTrieKeys[len(largeTrieKeys)-30], // Set start 30 keys from the end of the large trie
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)

				return response
			},
			requireResponse: func(t *testing.T, response message.LeafsResponse) {
				require.False(t, response.More)
				require.Len(t, response.Keys, 30)
				require.Len(t, response.Vals, 30)
			},
		},
		"full response for intermediate range of trie (less than leaf limit)": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				largeTrieKeys[1000], // Set the range for 1000 leafs in an intermediate range of the trie
				largeTrieKeys[1099], // (inclusive range)
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)

				return response
			},
			requireResponse: func(t *testing.T, response message.LeafsResponse) {
				require.True(t, response.More)
				require.Len(t, response.Keys, 100)
				require.Len(t, response.Vals, 100)
			},
		},
		"removed first key in response": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				bytes.Repeat([]byte{0x00}, common.HashLength),
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)

				var leafResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafResponse)
				require.NoError(t, err)
				leafResponse.Keys = leafResponse.Keys[1:]
				leafResponse.Vals = leafResponse.Vals[1:]

				modifiedResponse, err := message.CorethCodec.Marshal(message.Version, leafResponse)
				require.NoError(t, err)
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"removed first key in response and replaced proof": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				bytes.Repeat([]byte{0x00}, common.HashLength),
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)
				var leafResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafResponse)
				require.NoError(t, err)
				modifiedRequest := newLeafsRequest(
					request.RootHash(),
					request.AccountHash(),
					leafResponse.Keys[1],
					request.EndKey(),
					request.LimitValue(),
					request.NodeTypeValue(),
				)
				modifiedResponse, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 2, modifiedRequest)
				require.NoError(t, err)
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"removed last key in response": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				bytes.Repeat([]byte{0x00}, common.HashLength),
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)
				var leafResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafResponse)
				require.NoError(t, err)
				leafResponse.Keys = leafResponse.Keys[:len(leafResponse.Keys)-2]
				leafResponse.Vals = leafResponse.Vals[:len(leafResponse.Vals)-2]

				modifiedResponse, err := message.CorethCodec.Marshal(message.Version, leafResponse)
				require.NoError(t, err)
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"removed key from middle of response": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				bytes.Repeat([]byte{0x00}, common.HashLength),
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)
				var leafResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafResponse)
				require.NoError(t, err)
				// Remove middle key-value pair response
				leafResponse.Keys = append(leafResponse.Keys[:100], leafResponse.Keys[101:]...)
				leafResponse.Vals = append(leafResponse.Vals[:100], leafResponse.Vals[101:]...)

				modifiedResponse, err := message.CorethCodec.Marshal(message.Version, leafResponse)
				require.NoError(t, err)
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"corrupted value in middle of response": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				bytes.Repeat([]byte{0x00}, common.HashLength),
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)
				var leafResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafResponse)
				require.NoError(t, err)
				// Remove middle key-value pair response
				leafResponse.Vals[100] = []byte("garbage value data")

				modifiedResponse, err := message.CorethCodec.Marshal(message.Version, leafResponse)
				require.NoError(t, err)
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
		"all proof keys removed from response": {
			request: newLeafsRequest(
				largeTrieRoot,
				common.Hash{},
				bytes.Repeat([]byte{0x00}, common.HashLength),
				bytes.Repeat([]byte{0xff}, common.HashLength),
				leafsLimit,
				message.StateTrieNode,
			),
			getResponse: func(t *testing.T, request message.LeafsRequest) []byte {
				response, err := handler.OnLeafsRequest(t.Context(), ids.GenerateTestNodeID(), 1, request)
				require.NoError(t, err)
				require.NotEmpty(t, response)

				var leafResponse message.LeafsResponse
				_, err = message.CorethCodec.Unmarshal(response, &leafResponse)
				require.NoError(t, err)
				// Remove the proof
				leafResponse.ProofVals = nil

				modifiedResponse, err := message.CorethCodec.Marshal(message.Version, leafResponse)
				require.NoError(t, err)
				return modifiedResponse
			},
			expectedErr: errInvalidRangeProof,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := New(&Config{
				NetworkClient:    &testNetwork{},
				Codec:            message.CorethCodec,
				Stats:            clientstats.NewNoOpStats(),
				StateSyncNodeIDs: nil,
				BlockParser:      newTestBlockParser(),
			})
			responseBytes := test.getResponse(t, test.request)

			response, _, err := parseLeafsResponse(client.codec, test.request, responseBytes)
			require.ErrorIs(t, err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}

			leafsResponse, ok := response.(message.LeafsResponse)
			require.True(t, ok, "expected leafs response")
			test.requireResponse(t, leafsResponse)
		})
	}
}

func TestGetLeafsRetries(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	trieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	root, _, _ := synctest.GenerateIndependentTrie(t, r, trieDB, 100_000, common.HashLength)

	handler := handlers.NewLeafsRequestHandler(
		trieDB,
		message.StateTrieKeyLength,
		nil,
		message.CorethCodec,
		handlerstats.NewNoopHandlerStats(),
	)
	testNetClient := &testNetwork{}

	const maxAttempts = 8
	client := New(&Config{
		NetworkClient:    testNetClient,
		Codec:            message.CorethCodec,
		Stats:            clientstats.NewNoOpStats(),
		StateSyncNodeIDs: nil,
		BlockParser:      newTestBlockParser(),
	})

	request := newLeafsRequest(
		root,
		common.Hash{},
		bytes.Repeat([]byte{0x00}, common.HashLength),
		bytes.Repeat([]byte{0xff}, common.HashLength),
		1024,
		message.StateTrieNode,
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	goodResponse, responseErr := handler.OnLeafsRequest(ctx, ids.GenerateTestNodeID(), 1, request)
	require.NoError(t, responseErr)
	testNetClient.testResponse(1, nil, goodResponse)

	res, err := client.GetLeafs(ctx, request)
	require.NoError(t, err)
	require.Len(t, res.Keys, 1024)
	require.Len(t, res.Vals, 1024)

	// Succeeds within the allotted number of attempts
	invalidResponse := []byte("invalid response")
	testNetClient.testResponses(nil, invalidResponse, invalidResponse, goodResponse)

	res, err = client.GetLeafs(ctx, request)
	require.NoError(t, err)
	require.Len(t, res.Keys, 1024)
	require.Len(t, res.Vals, 1024)

	// Test that GetLeafs stops after the context is cancelled
	numAttempts := 0
	testNetClient.testResponse(maxAttempts, func() {
		numAttempts++
		if numAttempts >= maxAttempts {
			cancel()
		}
	}, invalidResponse)
	_, err = client.GetLeafs(ctx, request)
	require.ErrorIs(t, err, context.Canceled)
}

func TestStateSyncNodes(t *testing.T) {
	testNetClient := &testNetwork{}

	stateSyncNodes := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}
	client := New(&Config{
		NetworkClient:    testNetClient,
		Codec:            message.CorethCodec,
		Stats:            clientstats.NewNoOpStats(),
		StateSyncNodeIDs: stateSyncNodes,
		BlockParser:      newTestBlockParser(),
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempt := 0
	responses := [][]byte{{1}, {2}, {3}, {4}}
	testNetClient.testResponses(func() {
		attempt++
		if attempt >= 4 {
			cancel()
		}
	}, responses...)

	// send some request, doesn't matter what it is because we're testing the interaction with state sync nodes here
	response, err := client.GetLeafs(ctx, newEmptyLeafsRequest())
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, response)

	// require all nodes were called
	require.Contains(t, testNetClient.nodesRequested, stateSyncNodes[0])
	require.Contains(t, testNetClient.nodesRequested, stateSyncNodes[1])
	require.Contains(t, testNetClient.nodesRequested, stateSyncNodes[2])
	require.Contains(t, testNetClient.nodesRequested, stateSyncNodes[3])
}

func newLeafsRequest(
	root common.Hash,
	account common.Hash,
	start, end []byte,
	limit uint16,
	nodeType message.NodeType,
) message.LeafsRequest {
	return message.NewLeafsRequest(message.LeafsRequestTypeForCodec(message.CorethCodec), root, account, start, end, limit, nodeType)
}

func newEmptyLeafsRequest() message.LeafsRequest {
	return message.NewEmptyLeafsRequest(message.LeafsRequestTypeForCodec(message.CorethCodec))
}
