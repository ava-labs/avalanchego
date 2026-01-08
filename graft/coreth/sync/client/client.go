// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
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
	"github.com/ava-labs/avalanchego/graft/coreth/network"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/client/stats"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"

	ethparams "github.com/ava-labs/libevm/params"
)

const (
	baseRetryInterval = 10 * time.Millisecond // Initial retry interval
	maxRetryInterval  = 1 * time.Second       // Maximum retry interval (cap exponential backoff)

	epsilon = 1e-6 // small amount to add to time to avoid division by 0

	// Peer scoring constants
	minRequestsForScoring = 5                // Minimum requests before using peer scoring
	scoringDecayFactor    = 0.9              // Exponential moving average weight for response time
	stickyPeerDuration    = 30 * time.Second // How long to prefer the same peer
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

// peerStats tracks performance metrics for a single peer to enable intelligent peer selection.
type peerStats struct {
	nodeID          ids.NodeID
	successCount    atomic.Uint64  // Total successful requests
	failureCount    atomic.Uint64  // Total failed requests
	avgResponseTime atomic.Uint64  // Exponential moving average response time in nanoseconds
	lastUsed        atomic.Int64   // Unix nanoseconds when peer was last used
	totalRequests   atomic.Uint64  // Total requests (success + failure)
}

// score calculates a peer's quality score based on success rate and response time.
// Higher score = better peer. Returns 0 for peers with insufficient data.
func (p *peerStats) score() float64 {
	total := p.totalRequests.Load()
	if total < minRequestsForScoring {
		return 0 // Not enough data to score reliably
	}

	success := p.successCount.Load()
	avgTime := p.avgResponseTime.Load()

	// Calculate success rate (0.0 to 1.0)
	successRate := float64(success) / float64(total)

	// Calculate score: success rate divided by response time
	// Better peers have high success rate and low response time
	if avgTime == 0 {
		return successRate // Avoid division by zero
	}

	// Convert nanoseconds to seconds for more reasonable numbers
	avgTimeSeconds := float64(avgTime) / 1e9

	// Score = success_rate / response_time
	// Example: 95% success, 0.1s response = 9.5 score
	//          90% success, 0.2s response = 4.5 score
	return successRate / (avgTimeSeconds + epsilon)
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
}

// parseResponseFn parses given response bytes in context of specified request
// Validates response in context of the request
// Ensures the returned interface matches the expected response type of the request
// Returns the number of elements in the response (specific to the response type, used in metrics)
type parseResponseFn func(codec codec.Manager, request message.Request, response []byte) (interface{}, int, error)

type client struct {
	networkClient    network.SyncedNetworkClient
	codec            codec.Manager
	stateSyncNodes   []ids.NodeID
	stateSyncNodeIdx uint32 // Fallback round-robin index for peers without sufficient stats
	stats            stats.ClientSyncerStats
	blockParser      EthBlockParser

	// Peer performance tracking for intelligent peer selection
	peerStats     map[ids.NodeID]*peerStats
	peerStatsLock sync.RWMutex
	stickyPeer    atomic.Value // *ids.NodeID - currently preferred peer for sticky affinity
	stickyUntil   atomic.Int64 // Unix nanoseconds until which sticky peer is preferred
}

type ClientConfig struct {
	NetworkClient    network.SyncedNetworkClient
	Codec            codec.Manager
	Stats            stats.ClientSyncerStats
	StateSyncNodeIDs []ids.NodeID
	BlockParser      EthBlockParser
}

type EthBlockParser interface {
	ParseEthBlock(b []byte) (*types.Block, error)
}

func NewClient(config *ClientConfig) *client {
	c := &client{
		networkClient:  config.NetworkClient,
		codec:          config.Codec,
		stats:          config.Stats,
		stateSyncNodes: config.StateSyncNodeIDs,
		blockParser:    config.BlockParser,
		peerStats:      make(map[ids.NodeID]*peerStats),
	}

	// Initialize peer stats for all known state sync nodes
	for _, nodeID := range config.StateSyncNodeIDs {
		c.peerStats[nodeID] = &peerStats{
			nodeID: nodeID,
		}
	}

	return c
}

// calculateRetryBackoff returns exponential backoff duration for retry attempts.
// First retry is quick (10ms), then exponentially increases: 20ms, 40ms, 80ms, etc.
// Capped at maxRetryInterval (1 second) to avoid excessive delays.
func calculateRetryBackoff(attempt int) time.Duration {
	if attempt == 0 {
		return baseRetryInterval // First retry: quick 10ms
	}
	// Exponential backoff: 10ms * 2^attempt, capped at 1 second
	backoff := baseRetryInterval * (1 << uint(attempt))
	if backoff > maxRetryInterval {
		return maxRetryInterval
	}
	return backoff
}

// selectPeer chooses the best peer for the next request using scoring and sticky affinity.
// Returns ids.EmptyNodeID if no suitable peer is available.
func (c *client) selectPeer() ids.NodeID {
	if len(c.stateSyncNodes) == 0 {
		return ids.EmptyNodeID
	}

	// Check if we have a sticky peer that's still valid
	now := time.Now().UnixNano()
	if stickyUntil := c.stickyUntil.Load(); now < stickyUntil {
		if stickyPeerIntf := c.stickyPeer.Load(); stickyPeerIntf != nil {
			stickyPeer := stickyPeerIntf.(*ids.NodeID)
			return *stickyPeer
		}
	}

	c.peerStatsLock.RLock()
	defer c.peerStatsLock.RUnlock()

	var (
		bestPeer  ids.NodeID
		bestScore float64
	)

	// Find peer with highest score
	for nodeID, stats := range c.peerStats {
		score := stats.score()
		if score > bestScore {
			bestScore = score
			bestPeer = nodeID
		}
	}

	// If no peer has sufficient data for scoring, fall back to round-robin
	if bestScore == 0 {
		nodeIdx := atomic.AddUint32(&c.stateSyncNodeIdx, 1)
		return c.stateSyncNodes[nodeIdx%uint32(len(c.stateSyncNodes))]
	}

	// Set sticky peer for the next duration
	c.stickyPeer.Store(&bestPeer)
	c.stickyUntil.Store(now + int64(stickyPeerDuration))

	return bestPeer
}

// recordPeerResponse updates peer statistics based on request outcome.
func (c *client) recordPeerResponse(nodeID ids.NodeID, duration time.Duration, success bool) {
	if nodeID == ids.EmptyNodeID {
		return
	}

	c.peerStatsLock.Lock()
	stats, ok := c.peerStats[nodeID]
	if !ok {
		// Create stats for new peer
		stats = &peerStats{nodeID: nodeID}
		c.peerStats[nodeID] = stats
	}
	c.peerStatsLock.Unlock()

	// Update atomic counters
	stats.totalRequests.Add(1)
	if success {
		stats.successCount.Add(1)
	} else {
		stats.failureCount.Add(1)
	}

	// Update exponential moving average of response time
	oldAvg := stats.avgResponseTime.Load()
	newSample := uint64(duration.Nanoseconds())

	var newAvg uint64
	if oldAvg == 0 {
		// First sample
		newAvg = newSample
	} else {
		// Exponential moving average: new = old * decay + sample * (1 - decay)
		newAvg = uint64(float64(oldAvg)*scoringDecayFactor + float64(newSample)*(1-scoringDecayFactor))
	}
	stats.avgResponseTime.Store(newAvg)

	stats.lastUsed.Store(time.Now().UnixNano())
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
	if len(leafsResponse.Keys) > int(leafsRequest.Limit) || len(leafsResponse.Vals) > int(leafsRequest.Limit) {
		return nil, 0, fmt.Errorf("%w: (%d) > %d)", errTooManyLeaves, len(leafsResponse.Keys), leafsRequest.Limit)
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

	firstKey := leafsRequest.Start
	if len(leafsResponse.Keys) > 0 {
		lastKey := leafsResponse.Keys[len(leafsResponse.Keys)-1]

		if firstKey == nil {
			firstKey = bytes.Repeat([]byte{0x00}, len(lastKey))
		}
	}

	// VerifyRangeProof verifies that the key-value pairs included in [leafResponse] are all of the keys within the range from start
	// to the last key returned.
	// Also ensures the keys are in monotonically increasing order
	more, err := trie.VerifyRangeProof(leafsRequest.Root, firstKey, leafsResponse.Keys, leafsResponse.Vals, proof)
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
			response, nodeID, err = c.networkClient.SendSyncedAppRequestAny(ctx, StateSyncVersion, requestBytes)
		} else {
			// Use peer scoring and sticky affinity for intelligent peer selection
			nodeID = c.selectPeer()
			response, err = c.networkClient.SendSyncedAppRequest(ctx, nodeID, requestBytes)
		}
		duration := time.Since(start)
		metric.UpdateRequestLatency(duration)

		if err != nil {
			ctx := make([]interface{}, 0, 8)
			if nodeID != ids.EmptyNodeID {
				ctx = append(ctx, "nodeID", nodeID)
			}
			ctx = append(ctx, "attempt", attempt, "request", request, "err", err)
			log.Debug("request failed, retrying", ctx...)
			metric.IncFailed()
			c.networkClient.TrackBandwidth(nodeID, 0)
			c.recordPeerResponse(nodeID, duration, false) // Record network failure
			time.Sleep(calculateRetryBackoff(attempt))
			continue
		} else {
			responseIntf, numElements, err = parseFn(c.codec, request, response)
			if err != nil {
				lastErr = err
				log.Debug("could not validate response, retrying", "nodeID", nodeID, "attempt", attempt, "request", request, "err", err)
				c.networkClient.TrackBandwidth(nodeID, 0)
				metric.IncFailed()
				metric.IncInvalidResponse()
				c.recordPeerResponse(nodeID, duration, false) // Record invalid response
				continue
			}

			bandwidth := float64(len(response)) / (time.Since(start).Seconds() + epsilon)
			c.networkClient.TrackBandwidth(nodeID, bandwidth)
			metric.IncSucceeded()
			metric.IncReceived(int64(numElements))
			c.recordPeerResponse(nodeID, duration, true) // Record success
			return responseIntf, nil
		}
	}
}
