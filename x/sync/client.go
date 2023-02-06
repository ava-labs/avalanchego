// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

const (
	failedRequestSleepInterval = 10 * time.Millisecond

	epsilon = 1e-6 // small amount to add to time to avoid division by 0
)

var (
	_ Client = &client{}

	StateSyncVersion = &version.Application{
		Major: 1,
		Minor: 7,
		Patch: 13,
	}
	errInvalidRangeProof = errors.New("failed to verify range proof")
	errTooManyLeaves     = errors.New("response contains more than requested leaves")
)

// Client synchronously fetches data from the network to fulfill state sync requests.
// Repeatedly retries failed requests until the context is canceled.
type Client interface {
	// GetRangeProof synchronously sends the given request, returning a parsed StateResponse or error
	// Note: this verifies the response including the range proof.
	GetRangeProof(ctx context.Context, request *RangeProofRequest) (*merkledb.RangeProof, error)
	// GetChangeProof synchronously sends the given request, returning a parsed ChangesResponse or error
	// [verificationDB] is the local db that has all key/values in it for the proof's startroot within the proof's key range
	// Note: this verifies the response including the change proof.
	GetChangeProof(ctx context.Context, request *ChangeProofRequest, verificationDB *merkledb.Database) (*merkledb.ChangeProof, error)
}

type client struct {
	networkClient    NetworkClient
	stateSyncNodes   []ids.NodeID
	stateSyncNodeIdx uint32
	log              logging.Logger
	metrics          SyncMetrics
}

type ClientConfig struct {
	NetworkClient    NetworkClient
	StateSyncNodeIDs []ids.NodeID
	Log              logging.Logger
	Metrics          SyncMetrics
}

func NewClient(config *ClientConfig) Client {
	c := &client{
		networkClient:  config.NetworkClient,
		stateSyncNodes: config.StateSyncNodeIDs,
		log:            config.Log,
		metrics:        config.Metrics,
	}
	return c
}

// GetChangeProof synchronously retrieves the change proof given by [req].
// Upon failure, retries until the context is expired.
// The returned change proof is verified.
func (c *client) GetChangeProof(ctx context.Context, req *ChangeProofRequest, db *merkledb.Database) (*merkledb.ChangeProof, error) {
	parseFn := func(ctx context.Context, responseBytes []byte) (*merkledb.ChangeProof, error) {
		changeProof := &merkledb.ChangeProof{}
		if _, err := merkledb.Codec.DecodeChangeProof(responseBytes, changeProof); err != nil {
			return nil, err
		}

		// Ensure the response does not contain more than the requested number of leaves
		// and the start and end roots match the requested roots.
		if len(changeProof.KeyValues)+len(changeProof.DeletedKeys) > int(req.Limit) {
			return nil, fmt.Errorf("%w: (%d) > %d)", errTooManyLeaves, len(changeProof.KeyValues), req.Limit)
		}

		if err := changeProof.Verify(ctx, db, req.Start, req.End, req.EndingRoot); err != nil {
			return nil, fmt.Errorf("%s due to %w", errInvalidRangeProof, err)
		}
		return changeProof, nil
	}
	return getAndParse(ctx, c, req, parseFn)
}

// GetRangeProof synchronously retrieves the range proof given by [req].
// Upon failure, retries until the context is expired.
// The returned range proof is verified.
func (c *client) GetRangeProof(ctx context.Context, req *RangeProofRequest) (*merkledb.RangeProof, error) {
	parseFn := func(ctx context.Context, responseBytes []byte) (*merkledb.RangeProof, error) {
		rangeProof := &merkledb.RangeProof{}
		if _, err := merkledb.Codec.DecodeRangeProof(responseBytes, rangeProof); err != nil {
			return nil, err
		}

		// Ensure the response does not contain more than the maximum requested number of leaves.
		if len(rangeProof.KeyValues) > int(req.Limit) {
			return nil, fmt.Errorf("%w: (%d) > %d)", errTooManyLeaves, len(rangeProof.KeyValues), req.Limit)
		}

		if err := rangeProof.Verify(
			ctx,
			req.Start,
			req.End,
			req.Root,
		); err != nil {
			return nil, fmt.Errorf("%s due to %w", errInvalidRangeProof, err)
		}
		return rangeProof, nil
	}
	return getAndParse(ctx, c, req, parseFn)
}

// getAndParse uses [client] to send [request] to an arbitrary peer. If the peer responds,
// [parseFn] is called with the raw response. If [parseFn] returns an error or the request
// times out, this function will retry the request to a different peer until [ctx] expires.
// If [parseFn] returns a nil error, the result is returned from getAndParse.
func getAndParse[T any](ctx context.Context, client *client, request Request, parseFn func(context.Context, []byte) (*T, error)) (*T, error) {
	// marshal the request into requestBytes
	requestBytes, err := syncCodec.Marshal(Version, &request)
	if err != nil {
		return nil, err
	}

	var (
		lastErr  error
		response *T
	)
	// Loop until the context is cancelled or we get a valid response.
	for attempt := 0; ; attempt++ {
		// If the context has finished, return the context error early.
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return nil, fmt.Errorf("request failed after %d attempts with last error %w and ctx error %s", attempt, lastErr, err)
			}
			return nil, err
		}
		responseBytes, nodeID, err := client.get(ctx, requestBytes)
		if err == nil {
			if response, err = parseFn(ctx, responseBytes); err == nil {
				return response, nil
			}
		}

		client.log.Debug("request failed, retrying",
			zap.Stringer("nodeID", nodeID),
			zap.Int("attempt", attempt),
			zap.Stringer("request", request),
			zap.Error(err))

		if err != ctx.Err() {
			// if [err] is being propagated from [ctx], avoid overwriting [lastErr].
			lastErr = err
			time.Sleep(failedRequestSleepInterval)
		}
	}
}

// get sends [request] to an arbitrary peer and blocks until the node receives a response
// or [ctx] expires. Returns the raw response from the peer, the peer's NodeID, and an
// error if the request timed out. Thread safe.
func (c *client) get(ctx context.Context, requestBytes []byte) ([]byte, ids.NodeID, error) {
	c.metrics.RequestMade()
	var (
		response  []byte
		nodeID    ids.NodeID
		err       error
		startTime = time.Now()
	)
	if len(c.stateSyncNodes) == 0 {
		response, nodeID, err = c.networkClient.RequestAny(ctx, StateSyncVersion, requestBytes)
	} else {
		// get the next nodeID using the nodeIdx offset. If we're out of nodes, loop back to 0
		// we do this every attempt to ensure we get a different node each time if possible.
		nodeIdx := atomic.AddUint32(&c.stateSyncNodeIdx, 1)
		nodeID = c.stateSyncNodes[nodeIdx%uint32(len(c.stateSyncNodes))]
		response, err = c.networkClient.Request(ctx, nodeID, requestBytes)
	}
	if err != nil {
		c.metrics.RequestFailed()
		c.networkClient.TrackBandwidth(nodeID, 0)
		return response, nodeID, err
	}

	bandwidth := float64(len(response)) / (time.Since(startTime).Seconds() + epsilon)
	c.networkClient.TrackBandwidth(nodeID, bandwidth)
	c.metrics.RequestSucceeded()
	return response, nodeID, nil
}
