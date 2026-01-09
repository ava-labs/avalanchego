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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/network"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/sync/client/stats"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"

	ethparams "github.com/ava-labs/libevm/params"
)

const (
	failedRequestSleepInterval = 10 * time.Millisecond
	peerBlacklistDuration      = 2 * time.Minute  // How long to avoid a peer after consecutive failures
	maxConsecutiveFailures     = 3                // Consecutive failures before blacklisting
	perRequestTimeout          = 30 * time.Second // Timeout for individual request attempts
	maxRetriesPerRequest       = 50               // Maximum retries before giving up on a request
	retryWarningThreshold      = 20               // Log warning when retries exceed this

	epsilon = 1e-6 // small amount to add to time to avoid division by 0
)

var (
	errTooManyRetries = errors.New("request failed after too many retries")
)

// exponentialBackoff calculates exponential backoff delay with a maximum cap.
// Starts at baseDelay and doubles with each attempt, capped at 5 seconds.
// This reduces time wasted on repeatedly failing peers.
func exponentialBackoff(attempt int, baseDelay time.Duration) time.Duration {
	// Cap exponent at 9 to prevent overflow (2^9 = 512)
	exp := attempt
	if exp > 9 {
		exp = 9
	}

	delay := baseDelay * time.Duration(1<<exp) // 2^attempt

	// Cap at 5 seconds to avoid excessive delays
	maxDelay := 5 * time.Second
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

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

// peerFailureTracker tracks consecutive failures per peer for blacklisting
type peerFailureTracker struct {
	failures      map[ids.NodeID]int
	blacklistUntil map[ids.NodeID]time.Time
	lock          sync.RWMutex
}

func newPeerFailureTracker() *peerFailureTracker {
	return &peerFailureTracker{
		failures:      make(map[ids.NodeID]int),
		blacklistUntil: make(map[ids.NodeID]time.Time),
	}
}

func (p *peerFailureTracker) recordFailure(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.failures[nodeID]++
	if p.failures[nodeID] >= maxConsecutiveFailures {
		p.blacklistUntil[nodeID] = time.Now().Add(peerBlacklistDuration)
		log.Debug("peer blacklisted temporarily", "nodeID", nodeID, "failures", p.failures[nodeID], "until", p.blacklistUntil[nodeID])
	}
}

func (p *peerFailureTracker) recordSuccess(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	delete(p.failures, nodeID)
	delete(p.blacklistUntil, nodeID)
}

func (p *peerFailureTracker) isBlacklisted(nodeID ids.NodeID) bool {
	p.lock.RLock()
	defer p.lock.RUnlock()

	until, exists := p.blacklistUntil[nodeID]
	if !exists {
		return false
	}
	if time.Now().After(until) {
		// Blacklist expired, clean up in a deferred goroutine to avoid holding lock
		go func() {
			p.lock.Lock()
			defer p.lock.Unlock()
			delete(p.blacklistUntil, nodeID)
			delete(p.failures, nodeID)
		}()
		return false
	}
	return true
}

// peerMetrics tracks performance metrics for a peer
type peerMetrics struct {
	avgLatency     time.Duration // exponential moving average of request latency
	successCount   int64         // number of successful requests
	requestCount   int64         // total number of requests
	lastUpdateTime time.Time     // last time metrics were updated
}

// successRate calculates the success rate as a float between 0 and 1
func (m *peerMetrics) successRate() float64 {
	if m.requestCount == 0 {
		return 1.0 // no data yet, assume good
	}
	return float64(m.successCount) / float64(m.requestCount)
}

// peerMetricsTracker tracks performance metrics for all peers to enable intelligent peer selection
type peerMetricsTracker struct {
	metrics map[ids.NodeID]*peerMetrics
	lock    sync.RWMutex
}

func newPeerMetricsTracker() *peerMetricsTracker {
	return &peerMetricsTracker{
		metrics: make(map[ids.NodeID]*peerMetrics),
	}
}

// recordRequest records the latency and outcome of a request
func (p *peerMetricsTracker) recordRequest(nodeID ids.NodeID, latency time.Duration, success bool) {
	p.lock.Lock()
	defer p.lock.Unlock()

	m, exists := p.metrics[nodeID]
	if !exists {
		m = &peerMetrics{
			avgLatency:     latency,
			successCount:   0,
			requestCount:   0,
			lastUpdateTime: time.Now(),
		}
		p.metrics[nodeID] = m
	}

	// Update exponential moving average: new_avg = 0.7 * old_avg + 0.3 * new_value
	// This gives more weight to recent measurements while smoothing out spikes
	const alpha = 0.3
	m.avgLatency = time.Duration(float64(m.avgLatency)*(1-alpha) + float64(latency)*alpha)

	m.requestCount++
	if success {
		m.successCount++
	}
	m.lastUpdateTime = time.Now()
}

// selectBestPeer selects the peer with the best combination of low latency and high success rate
// from the provided list of candidate peers. Returns the zero NodeID if no suitable peer found.
func (p *peerMetricsTracker) selectBestPeer(candidates []ids.NodeID) ids.NodeID {
	if len(candidates) == 0 {
		return ids.NodeID{}
	}

	p.lock.RLock()
	defer p.lock.RUnlock()

	var bestPeer ids.NodeID
	var bestScore float64 = -1

	for _, nodeID := range candidates {
		m, exists := p.metrics[nodeID]
		if !exists {
			// No metrics for this peer yet - consider it as a candidate with neutral score
			// This ensures new peers get tried
			score := 0.5
			if score > bestScore {
				bestScore = score
				bestPeer = nodeID
			}
			continue
		}

		// Calculate score: higher is better
		// Score combines success rate (0-1) and inverse latency
		// Normalize latency to 0-1 scale where 100ms = 1.0 (excellent), 1s = 0.1 (poor)
		successRate := m.successRate()
		latencyMs := float64(m.avgLatency.Milliseconds())
		latencyScore := 100.0 / (100.0 + latencyMs) // 100ms -> 0.5, 1000ms -> 0.09

		// Weight: 70% success rate, 30% latency
		score := 0.7*successRate + 0.3*latencyScore

		if score > bestScore {
			bestScore = score
			bestPeer = nodeID
		}
	}

	return bestPeer
}

type client struct {
	networkClient    network.SyncedNetworkClient
	codec            codec.Manager
	stateSyncNodes   []ids.NodeID
	stateSyncNodeIdx uint32
	stats            stats.ClientSyncerStats
	blockParser      EthBlockParser
	peerTracker      *peerFailureTracker
	metricsTracker   *peerMetricsTracker // tracks latency and success rate for intelligent peer selection
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
	return &client{
		networkClient:  config.NetworkClient,
		codec:          config.Codec,
		stats:          config.Stats,
		stateSyncNodes: config.StateSyncNodeIDs,
		blockParser:    config.BlockParser,
		peerTracker:    newPeerFailureTracker(),
		metricsTracker: newPeerMetricsTracker(),
	}
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
	// Loop until the context is cancelled, we get a valid response, or hit retry limit
	for attempt := 0; ; attempt++ {
		// Check retry limit to prevent infinite loops
		if attempt >= maxRetriesPerRequest {
			return nil, fmt.Errorf("%w: %d attempts, last error: %v", errTooManyRetries, attempt, lastErr)
		}

		// Log warning when approaching retry limit
		if attempt == retryWarningThreshold {
			log.Warn("Request retry count high, may be stuck",
				"attempt", attempt,
				"maxRetries", maxRetriesPerRequest,
				"request", request,
				"lastErr", lastErr)
		}

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

		// Create a per-request timeout context to prevent hanging on slow peers
		// Use an anonymous function to ensure cancel() is called after each request
		func() {
			requestCtx, cancel := context.WithTimeout(ctx, perRequestTimeout)
			defer cancel()

			if len(c.stateSyncNodes) == 0 {
				response, nodeID, err = c.networkClient.SendSyncedAppRequestAny(requestCtx, StateSyncVersion, requestBytes)
			} else {
				// Intelligent peer selection: prefer peers with low latency and high success rate
				// Build list of non-blacklisted peers
				candidates := make([]ids.NodeID, 0, len(c.stateSyncNodes))
				for _, peer := range c.stateSyncNodes {
					if !c.peerTracker.isBlacklisted(peer) {
						candidates = append(candidates, peer)
					}
				}

				// If all peers are blacklisted, use all peers (stale blacklist recovery)
				if len(candidates) == 0 {
					log.Debug("all static peers blacklisted, using all peers anyway", "numPeers", len(c.stateSyncNodes))
					candidates = c.stateSyncNodes
				}

				// Select best peer based on latency and success rate metrics
				nodeID = c.metricsTracker.selectBestPeer(candidates)
				if nodeID == (ids.NodeID{}) {
					// No peer selected (shouldn't happen), fall back to round-robin
					nodeIdx := atomic.AddUint32(&c.stateSyncNodeIdx, 1)
					nodeID = c.stateSyncNodes[nodeIdx%uint32(len(c.stateSyncNodes))]
				}

				response, err = c.networkClient.SendSyncedAppRequest(requestCtx, nodeID, requestBytes)
			}
		}()

		metric.UpdateRequestLatency(time.Since(start))

		if err != nil {
			ctx := make([]interface{}, 0, 8)
			if nodeID != ids.EmptyNodeID {
				ctx = append(ctx, "nodeID", nodeID)
				c.peerTracker.recordFailure(nodeID)
				c.metricsTracker.recordRequest(nodeID, time.Since(start), false) // Record failed request
			}
			ctx = append(ctx, "attempt", attempt, "request", request, "err", err)
			log.Debug("request failed, retrying", ctx...)
			metric.IncFailed()
			c.networkClient.TrackBandwidth(nodeID, 0)
			// Use exponential backoff to avoid wasting time on repeatedly failing peers
		backoff := exponentialBackoff(attempt, failedRequestSleepInterval)
		time.Sleep(backoff)
			continue
		} else {
			responseIntf, numElements, err = parseFn(c.codec, request, response)
			if err != nil {
				lastErr = err
				log.Debug("could not validate response, retrying", "nodeID", nodeID, "attempt", attempt, "request", request, "err", err)
				c.networkClient.TrackBandwidth(nodeID, 0)
				c.peerTracker.recordFailure(nodeID)
				c.metricsTracker.recordRequest(nodeID, time.Since(start), false) // Record failed validation
				metric.IncFailed()
				metric.IncInvalidResponse()
				continue
			}

			bandwidth := float64(len(response)) / (time.Since(start).Seconds() + epsilon)
			c.networkClient.TrackBandwidth(nodeID, bandwidth)
			c.peerTracker.recordSuccess(nodeID)
			c.metricsTracker.recordRequest(nodeID, time.Since(start), true) // Record successful request
			metric.IncSucceeded()
			metric.IncReceived(int64(numElements))
			return responseIntf, nil
		}
	}
}
