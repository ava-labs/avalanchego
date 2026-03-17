// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client/stats"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/version"

	ethparams "github.com/ava-labs/libevm/params"
)

const (
	failedRequestSleepInterval = 10 * time.Millisecond

	epsilon = 1e-6 // small amount to add to time to avoid division by 0
)

var (
	StateSyncVersion = &version.Application{
		Major: 1,
		Minor: 7,
		Patch: 13,
	}
	errEmptyResponse          = errors.New("empty response")
	errTooManyBlocks          = errors.New("response contains more blocks than requested")
	errHashMismatch           = errors.New("hash does not match expected value")
	errInvalidRangeProof      = errors.New("failed to verify range proof")
	errTooManyLeaves          = errors.New("response contains more than requested leaves")
	errUnmarshalResponse      = errors.New("failed to unmarshal response")
	errInvalidCodeResponseLen = errors.New("number of code bytes in response does not match requested hashes")
	errMaxCodeSizeExceeded    = errors.New("max code size exceeded")
)
var _ Client = (*client)(nil)

// Network defines the interface for sending sync requests over the network.
// This interface is implemented by the network layer in coreth and subnet-evm.
type Network interface {
	p2p.NodeSampler

	// SendSyncedAppRequestAny synchronously sends request to an arbitrary peer with a
	// node version greater than or equal to minVersion.
	// Returns response bytes, the ID of the chosen peer, and ErrRequestFailed if
	// the request should be retried.
	SendSyncedAppRequestAny(ctx context.Context, minVersion *version.Application, request []byte) ([]byte, ids.NodeID, error)

	// SendSyncedAppRequest synchronously sends request to the selected nodeID
	// Returns response bytes, and ErrRequestFailed if the request should be retried.
	SendSyncedAppRequest(ctx context.Context, nodeID ids.NodeID, request []byte) ([]byte, error)

	// TrackBandwidth should be called after receiving a response from a peer to
	// track performance. This is used to prioritize peers that are more responsive.
	TrackBandwidth(nodeID ids.NodeID, bandwidth float64)

	// P2PNetwork returns the unabstracted [p2p.Network].
	P2PNetwork() *p2p.Network
}

// Client synchronously fetches data from the network to fulfill state sync requests.
// Repeatedly requests failed requests until the context to the request is expired.
type Client interface {
	// GetLeafs synchronously sends the given request, returning a parsed LeafsResponse or error
	// Note: this verifies the response including the range proofs.
	GetLeafs(ctx context.Context, request message.LeafsRequest) (message.LeafsResponse, error)

	// GetBlocks synchronously retrieves blocks starting with specified common.Hash and height up to specified parents
	// specified range from height to height-parents is inclusive
	GetBlocks(ctx context.Context, blockHash common.Hash, height uint64, parents uint16) ([]*types.Block, error)

	// GetCode synchronously retrieves code associated with the given hashes
	GetCode(ctx context.Context, hashes []common.Hash) ([][]byte, error)

	// AddClient creates a separate client on the underlying [p2p.Network].
	AddClient(handlerID uint64) *p2p.Client

	// StateSyncNodes returns the list of nodes provided via config.
	StateSyncNodes() []ids.NodeID
}

// parseResponseFn parses given response bytes in context of specified request
// Validates response in context of the request
// Ensures the returned interface matches the expected response type of the request
// Returns the number of elements in the response (specific to the response type, used in metrics)
type parseResponseFn func(codec codec.Manager, request message.Request, response []byte) (interface{}, int, error)

type client struct {
	network          Network
	codec            codec.Manager
	stateSyncNodes   []ids.NodeID
	stateSyncNodeIdx uint32
	stats            stats.ClientSyncerStats
	blockParser      EthBlockParser
}

type Config struct {
	Network          Network
	Codec            codec.Manager
	Stats            stats.ClientSyncerStats
	StateSyncNodeIDs []ids.NodeID
	BlockParser      EthBlockParser
}

type EthBlockParser interface {
	ParseEthBlock(b []byte) (*types.Block, error)
}

func New(config *Config) *client {
	return &client{
		network:        config.Network,
		codec:          config.Codec,
		stats:          config.Stats,
		stateSyncNodes: config.StateSyncNodeIDs,
		blockParser:    config.BlockParser,
	}
}

func (c *client) AddClient(handlerID uint64) *p2p.Client {
	return c.network.P2PNetwork().NewClient(handlerID, c.network)
}

func (c *client) StateSyncNodes() []ids.NodeID {
	return c.stateSyncNodes
}

// GetLeafs synchronously retrieves leafs as per given [message.LeafsRequest]
// Retries when:
// - response bytes could not be unmarshalled to [message.LeafsResponse]
// - response keys do not correspond to the requested range.
// - response does not contain a valid merkle proof.
func (c *client) GetLeafs(ctx context.Context, req message.LeafsRequest) (message.LeafsResponse, error) {
	data, err := c.get(ctx, req, parseLeafsResponse)
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
	limit := leafsRequest.KeyLimit()
	if len(leafsResponse.Keys) > int(limit) || len(leafsResponse.Vals) > int(limit) {
		return nil, 0, fmt.Errorf("%w: (%d) > %d)", errTooManyLeaves, len(leafsResponse.Keys), limit)
	}

	// An empty response (no more keys) requires a merkle proof
	if len(leafsResponse.Keys) == 0 && len(leafsResponse.ProofVals) == 0 {
		return nil, 0, errors.New("empty key response must include merkle proof")
	}

	var proof ethdb.Database
	// Populate proof when ProofVals are present in the response. Its ok to pass it as nil to the trie.VerifyRangeProof
	// function as it will assert that all the leaves belonging to the specified root are present.
	if len(leafsResponse.ProofVals) > 0 {
		proof = rawdb.NewMemoryDatabase()
		defer proof.Close()
		for _, proofVal := range leafsResponse.ProofVals {
			proofKey := crypto.Keccak256(proofVal)
			if err := proof.Put(proofKey, proofVal); err != nil {
				return nil, 0, err
			}
		}
	}

	firstKey := leafsRequest.StartKey()
	if len(leafsResponse.Keys) > 0 {
		lastKey := leafsResponse.Keys[len(leafsResponse.Keys)-1]

		if firstKey == nil {
			firstKey = bytes.Repeat([]byte{0x00}, len(lastKey))
		}
	}

	// VerifyRangeProof verifies that the key-value pairs included in [leafResponse] are all of the keys within the range from start
	// to the last key returned.
	// Also ensures the keys are in monotonically increasing order
	more, err := trie.VerifyRangeProof(leafsRequest.RootHash(), firstKey, leafsResponse.Keys, leafsResponse.Vals, proof)
	if err != nil {
		return nil, 0, fmt.Errorf("%w due to %w", errInvalidRangeProof, err)
	}

	// Set the [More] flag to indicate if there are more leaves to the right of the last key in the response
	// that needs to be fetched.
	leafsResponse.More = more

	return leafsResponse, len(leafsResponse.Keys), nil
}

func (c *client) GetBlocks(ctx context.Context, hash common.Hash, height uint64, parents uint16) ([]*types.Block, error) {
	req := message.BlockRequest{
		Hash:    hash,
		Height:  height,
		Parents: parents,
	}

	data, err := c.get(ctx, req, c.parseBlocks)
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
		return nil, 0, fmt.Errorf("%w: %w", errUnmarshalResponse, err)
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
			return nil, 0, fmt.Errorf("%w: %w", errUnmarshalResponse, err)
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

func (c *client) GetCode(ctx context.Context, hashes []common.Hash) ([][]byte, error) {
	req := message.NewCodeRequest(hashes)

	data, err := c.get(ctx, req, parseCode)
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
		if len(code) > ethparams.MaxCodeSize {
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

// get submits given request and blockingly returns with either a parsed response object or an error
// if [ctx] expires before the client can successfully retrieve a valid response.
// Retries if there is a network error or if the [parseResponseFn] returns an error indicating an invalid response.
// Returns the parsed interface returned from [parseFn].
// Thread safe
func (c *client) get(ctx context.Context, request message.Request, parseFn parseResponseFn) (interface{}, error) {
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
		lastErr      error
	)
	// Loop until the context is cancelled or we get a valid response.
	for attempt := 0; ; attempt++ {
		// If the context has finished, return the context error early.
		if ctxErr := ctx.Err(); ctxErr != nil {
			if lastErr != nil {
				return nil, fmt.Errorf("request failed after %d attempts with last error %w and ctx error %w", attempt, lastErr, ctxErr)
			} else {
				return nil, ctxErr
			}
		}

		metric.IncRequested()

		var (
			response []byte
			nodeID   ids.NodeID
			start    = time.Now()
		)
		if len(c.stateSyncNodes) == 0 {
			response, nodeID, err = c.network.SendSyncedAppRequestAny(ctx, StateSyncVersion, requestBytes)
		} else {
			// get the next nodeID using the nodeIdx offset. If we're out of nodes, loop back to 0
			// we do this every attempt to ensure we get a different node each time if possible.
			nodeIdx := atomic.AddUint32(&c.stateSyncNodeIdx, 1)
			nodeID = c.stateSyncNodes[nodeIdx%uint32(len(c.stateSyncNodes))]

			response, err = c.network.SendSyncedAppRequest(ctx, nodeID, requestBytes)
		}
		metric.UpdateRequestLatency(time.Since(start))

		if err != nil {
			logCtx := make([]any, 0, 8)
			if nodeID != ids.EmptyNodeID {
				logCtx = append(logCtx, "nodeID", nodeID)
			}
			logCtx = append(logCtx, "attempt", attempt, "request", request, "err", err)
			log.Debug("request failed, retrying", logCtx...)
			metric.IncFailed()
			c.network.TrackBandwidth(nodeID, 0)
			time.Sleep(failedRequestSleepInterval)
			continue
		} else {
			responseIntf, numElements, err = parseFn(c.codec, request, response)
			if err != nil {
				lastErr = err
				log.Debug("could not validate response, retrying", "nodeID", nodeID, "attempt", attempt, "request", request, "err", err)
				c.network.TrackBandwidth(nodeID, 0)
				metric.IncFailed()
				metric.IncInvalidResponse()
				continue
			}

			bandwidth := float64(len(response)) / (time.Since(start).Seconds() + epsilon)
			c.network.TrackBandwidth(nodeID, bandwidth)
			metric.IncSucceeded()
			metric.IncReceived(int64(numElements))
			return responseIntf, nil
		}
	}
}
