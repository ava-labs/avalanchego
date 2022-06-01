// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/sync/client/stats"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

var (
	StateSyncVersion          = version.NewDefaultApplication(constants.PlatformName, 1, 7, 11)
	errEmptyResponse          = errors.New("empty response")
	errTooManyBlocks          = errors.New("response contains more blocks than requested")
	errHashMismatch           = errors.New("hash does not match expected value")
	errInvalidRangeProof      = errors.New("failed to verify range proof")
	errExceededRetryLimit     = errors.New("exceeded request retry limit")
	errTooManyLeaves          = errors.New("response contains more than requested leaves")
	errUnmarshalResponse      = errors.New("failed to unmarshal response")
	errInvalidCodeResponseLen = errors.New("number of code bytes in response does not match requested hashes")
	errMaxCodeSizeExceeded    = errors.New("max code size exceeded")
)
var _ Client = &client{}

// Client is a state sync client that synchronously fetches data from the network
type Client interface {
	// GetLeafs synchronously sends given request, returning parsed *LeafsResponse or error
	GetLeafs(request message.LeafsRequest) (message.LeafsResponse, error)

	// GetBlocks synchronously retrieves blocks starting with specified common.Hash and height up to specified parents
	// specified range from height to height-parents is inclusive
	GetBlocks(blockHash common.Hash, height uint64, parents uint16) ([]*types.Block, error)

	// GetCode synchronously retrieves code associated with the given hashes
	GetCode(hashes []common.Hash) ([][]byte, error)
}

// parseResponseFn parses given response bytes in context of specified request
// Validates response in context of the request
// Ensures the returned interface matches the expected response type of the request
// Returns the number of elements in the response (specific to the response type, used in metrics)
type parseResponseFn func(codec codec.Manager, request message.Request, response []byte) (interface{}, int, error)

type client struct {
	networkClient    peer.NetworkClient
	codec            codec.Manager
	maxAttempts      uint8
	maxRetryDelay    time.Duration
	stateSyncNodes   []ids.NodeID
	stateSyncNodeIdx uint32
	stats            stats.ClientSyncerStats
	blockParser      EthBlockParser
}

type ClientConfig struct {
	NetworkClient    peer.NetworkClient
	Codec            codec.Manager
	Stats            stats.ClientSyncerStats
	MaxAttempts      uint8
	MaxRetryDelay    time.Duration
	StateSyncNodeIDs []ids.NodeID
	BlockParser      EthBlockParser
}

type EthBlockParser interface {
	ParseEthBlock(b []byte) (*types.Block, error)
}

func NewClient(config *ClientConfig) *client {
	return &client{
		networkClient:  config.NetworkClient,
		codec:          config.Codec,
		stats:          config.Stats,
		maxAttempts:    config.MaxAttempts,
		maxRetryDelay:  config.MaxRetryDelay,
		stateSyncNodes: config.StateSyncNodeIDs,
		blockParser:    config.BlockParser,
	}
}

// GetLeafs synchronously retrieves leafs as per given [message.LeafsRequest]
// Retries when:
// - response bytes could not be unmarshalled to [message.LeafsResponse]
// - response keys do not correspond to the requested range.
// - response does not contain a valid merkle proof.
// Returns error if retries have been exceeded
func (c *client) GetLeafs(req message.LeafsRequest) (message.LeafsResponse, error) {
	data, err := c.get(req, parseLeafsResponse)
	if err != nil {
		return message.LeafsResponse{}, err
	}

	return data.(message.LeafsResponse), nil
}

// parseLeafsResponse validates given object as message.LeafsResponse
// assumes reqIntf is of type message.LeafsRequest
// returns a non-nil error if the request should be retried
// returns error when:
// - response bytes could not be unmarshalled into message.LeafsResponse
// - number of response keys is not equal to the response values
// - first and last key in the response is not within the requested start and end range
// - response keys are not in increasing order
// - proof validation failed
func parseLeafsResponse(codec codec.Manager, reqIntf message.Request, data []byte) (interface{}, int, error) {
	var leafsResponse message.LeafsResponse
	if _, err := codec.Unmarshal(data, &leafsResponse); err != nil {
		return nil, 0, err
	}

	leafsRequest := reqIntf.(message.LeafsRequest)

	// Ensure the response does not contain more than the maximum requested number of leaves.
	if len(leafsResponse.Keys) > int(leafsRequest.Limit) || len(leafsResponse.Vals) > int(leafsRequest.Limit) {
		return nil, 0, fmt.Errorf("%w: (%d) > %d)", errTooManyLeaves, len(leafsResponse.Keys), leafsRequest.Limit)
	}

	// An empty response (no more keys) requires a merkle proof
	if len(leafsResponse.Keys) == 0 && len(leafsResponse.ProofKeys) == 0 {
		return nil, 0, fmt.Errorf("empty key response must include merkle proof")
	}

	var proof ethdb.Database
	// Populate proof when ProofKeys are present in the response. Its ok to pass it as nil to the trie.VerifyRangeProof
	// function as it will assert that all the leaves belonging to the specified root are present.
	if len(leafsResponse.ProofKeys) > 0 {
		if len(leafsResponse.ProofKeys) != len(leafsResponse.ProofVals) {
			return nil, 0, fmt.Errorf("mismatch in length of proof keys (%d)/vals (%d)", len(leafsResponse.ProofKeys), len(leafsResponse.ProofVals))
		}
		proof = memorydb.New()
		defer proof.Close()
		for i, proofKey := range leafsResponse.ProofKeys {
			if err := proof.Put(proofKey, leafsResponse.ProofVals[i]); err != nil {
				return nil, 0, err
			}
		}
	}

	var (
		firstKey = leafsRequest.Start
		lastKey  = leafsRequest.End
	)
	// Last key is the last returned key in response
	if len(leafsResponse.Keys) > 0 {
		lastKey = leafsResponse.Keys[len(leafsResponse.Keys)-1]

		if firstKey == nil {
			firstKey = bytes.Repeat([]byte{0x00}, len(lastKey))
		}
	}

	// VerifyRangeProof verifies that the key-value pairs included in [leafResponse] are all of the keys within the range from start
	// to the last key returned.
	// Also ensures the keys are in monotonically increasing order
	more, err := trie.VerifyRangeProof(leafsRequest.Root, firstKey, lastKey, leafsResponse.Keys, leafsResponse.Vals, proof)
	if err != nil {
		return nil, 0, fmt.Errorf("%s due to %w", errInvalidRangeProof, err)
	}

	// Set the [More] flag to indicate if there are more leaves to the right of the last key in the response
	// that needs to be fetched.
	leafsResponse.More = more

	return leafsResponse, len(leafsResponse.Keys), nil
}

func (c *client) GetBlocks(hash common.Hash, height uint64, parents uint16) ([]*types.Block, error) {
	req := message.BlockRequest{
		Hash:    hash,
		Height:  height,
		Parents: parents,
	}

	data, err := c.get(req, c.parseBlocks)
	if err != nil {
		return nil, fmt.Errorf("could not get blocks (%s) due to %w", hash, err)
	}

	return data.(types.Blocks), nil
}

// parseBlocks validates given object as message.BlockResponse
// assumes req is of type message.BlockRequest
// returns types.Blocks as interface{}
// returns a non-nil error if the request should be retried
func (c *client) parseBlocks(codec codec.Manager, req message.Request, data []byte) (interface{}, int, error) {
	var response message.BlockResponse
	if _, err := codec.Unmarshal(data, &response); err != nil {
		return nil, 0, fmt.Errorf("%s: %w", errUnmarshalResponse, err)
	}
	if len(response.Blocks) == 0 {
		return nil, 0, errEmptyResponse
	}
	blockRequest := req.(message.BlockRequest)
	numParentsRequested := blockRequest.Parents
	if len(response.Blocks) > int(numParentsRequested) {
		return nil, 0, errTooManyBlocks
	}

	hash := blockRequest.Hash

	// attempt to decode blocks
	blocks := make(types.Blocks, len(response.Blocks))
	for i, blkBytes := range response.Blocks {
		block, err := c.blockParser.ParseEthBlock(blkBytes)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", errUnmarshalResponse, err)
		}

		if block.Hash() != hash {
			return nil, 0, fmt.Errorf("%w for block: (got %v) (expected %v)", errHashMismatch, block.Hash(), hash)
		}

		blocks[i] = block
		hash = block.ParentHash()
	}

	// return decoded blocks
	return blocks, len(blocks), nil
}

func (c *client) GetCode(hashes []common.Hash) ([][]byte, error) {
	req := message.NewCodeRequest(hashes)

	data, err := c.get(req, parseCode)
	if err != nil {
		return nil, fmt.Errorf("could not get code (%s): %w", req, err)
	}

	return data.([][]byte), nil
}

// parseCode validates given object as a code object
// assumes req is of type message.CodeRequest
// returns a non-nil error if the request should be retried
func parseCode(codec codec.Manager, req message.Request, data []byte) (interface{}, int, error) {
	var response message.CodeResponse
	if _, err := codec.Unmarshal(data, &response); err != nil {
		return nil, 0, err
	}

	codeRequest := req.(message.CodeRequest)
	if len(response.Data) != len(codeRequest.Hashes) {
		return nil, 0, fmt.Errorf("%w (got %d) (requested %d)", errInvalidCodeResponseLen, len(response.Data), len(codeRequest.Hashes))
	}

	totalBytes := 0
	for i, code := range response.Data {
		if len(code) > params.MaxCodeSize {
			return nil, 0, fmt.Errorf("%w: (hash %s) (size %d)", errMaxCodeSizeExceeded, codeRequest.Hashes[i], len(code))
		}

		hash := crypto.Keccak256Hash(code)
		if hash != codeRequest.Hashes[i] {
			return nil, 0, fmt.Errorf("%w for code at index %d: (got %v) (expected %v)", errHashMismatch, i, hash, codeRequest.Hashes[i])
		}
		totalBytes += len(code)
	}

	return response.Data, totalBytes, nil
}

// get submits given request and blockingly returns with either a parsed response object or error
// retry is made if there is a network error or if the [parseResponseFn] returns a non-nil error
// returns parsed struct as interface{} returned by parseResponseFn
// retries given request for maximum of [attempts] times with maximum delay of [maxRetryDelay] between attempts
// Thread safe
func (c *client) get(request message.Request, parseFn parseResponseFn) (interface{}, error) {
	// marshal the request into requestBytes
	requestBytes, err := message.RequestToBytes(c.codec, request)
	if err != nil {
		return nil, err
	}

	metric, err := c.stats.GetMetric(request)
	if err != nil {
		return nil, err
	}
	var (
		responseIntf interface{}
		numElements  int
	)
	// Loop until we run out of attempts or receive a valid response.
	for attempt := uint8(0); attempt < c.maxAttempts; attempt++ {
		// If this is a retry attempt, wait for random duration to ensure
		// that we do not spin through the maximum attempts during a period
		// where the node may not be well connected to the network.
		if attempt > 0 {
			randTime := rand.Int63n(c.maxRetryDelay.Nanoseconds())
			time.Sleep(time.Duration(randTime))
		}
		metric.IncRequested()

		var (
			response []byte
			nodeID   ids.NodeID
			start    time.Time = time.Now()
		)
		if len(c.stateSyncNodes) == 0 {
			response, nodeID, err = c.networkClient.RequestAny(StateSyncVersion, requestBytes)
		} else {
			// get the next nodeID using the nodeIdx offset. If we're out of nodes, loop back to 0
			// we do this every attempt to ensure we get a different node each time if possible.
			nodeIdx := atomic.AddUint32(&c.stateSyncNodeIdx, 1)
			nodeID = c.stateSyncNodes[nodeIdx%uint32(len(c.stateSyncNodes))]

			response, err = c.networkClient.Request(nodeID, requestBytes)
		}
		metric.UpdateRequestLatency(time.Since(start))

		if err != nil {
			ctx := make([]interface{}, 0, 8)
			if nodeID != ids.EmptyNodeID {
				ctx = append(ctx, "nodeID", nodeID)
			}
			ctx = append(ctx, "attempt", attempt, "request", request, "err", err)
			log.Debug("request failed, retrying", ctx...)
			metric.IncFailed()
			continue
		} else {
			responseIntf, numElements, err = parseFn(c.codec, request, response)
			if err != nil {
				log.Info("could not validate response, retrying", "nodeID", nodeID, "attempt", attempt, "request", request, "err", err)
				metric.IncFailed()
				metric.IncInvalidResponse()
				continue
			}
			metric.IncSucceeded()
			metric.UpdateReceived(int64(numElements))
			return responseIntf, nil
		}
	}

	// we only get this far if we've run out of attempts
	return nil, fmt.Errorf("%s (%d): %w", errExceededRetryLimit, c.maxAttempts, err)
}
