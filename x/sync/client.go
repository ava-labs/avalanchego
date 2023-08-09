// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	initialRetryWait = 10 * time.Millisecond
	maxRetryWait     = time.Second
	retryWaitFactor  = 1.5 // Larger --> timeout grows more quickly

	epsilon = 1e-6 // small amount to add to time to avoid division by 0
)

var (
	_ Client = (*client)(nil)

	errInvalidRangeProof = errors.New("failed to verify range proof")
	errTooManyKeys       = errors.New("response contains more than requested keys")
	errTooManyBytes      = errors.New("response contains more than requested bytes")
)

// Client synchronously fetches data from the network to fulfill state sync requests.
// Repeatedly retries failed requests until the context is canceled.
type Client interface {
	// GetRangeProof synchronously sends the given request, returning a parsed StateResponse or error
	// Note: this verifies the response including the range proof.
	GetRangeProof(ctx context.Context, request *pb.SyncGetRangeProofRequest) (*merkledb.RangeProof, error)
	// GetChangeProof synchronously sends the given request, returning a parsed ChangesResponse or error
	// [verificationDB] is the local db that has all key/values in it for the proof's startroot within the proof's key range
	// Note: this verifies the response including the change proof.
	GetChangeProof(ctx context.Context, request *pb.SyncGetChangeProofRequest, verificationDB DB) (*merkledb.ChangeProof, error)
}

type client struct {
	networkClient       NetworkClient
	stateSyncNodes      []ids.NodeID
	stateSyncNodeIdx    uint32
	stateSyncMinVersion *version.Application
	log                 logging.Logger
	metrics             SyncMetrics
}

type ClientConfig struct {
	NetworkClient       NetworkClient
	StateSyncNodeIDs    []ids.NodeID
	StateSyncMinVersion *version.Application
	Log                 logging.Logger
	Metrics             SyncMetrics
}

func NewClient(config *ClientConfig) Client {
	return &client{
		networkClient:       config.NetworkClient,
		stateSyncNodes:      config.StateSyncNodeIDs,
		stateSyncMinVersion: config.StateSyncMinVersion,
		log:                 config.Log,
		metrics:             config.Metrics,
	}
}

// GetChangeProof synchronously retrieves the change proof given by [req].
// Upon failure, retries until the context is expired.
// The returned change proof is verified.
func (c *client) GetChangeProof(ctx context.Context, req *pb.SyncGetChangeProofRequest, db DB) (*merkledb.ChangeProof, error) {
	parseFn := func(ctx context.Context, responseBytes []byte) (*merkledb.ChangeProof, error) {
		if len(responseBytes) > int(req.BytesLimit) {
			return nil, fmt.Errorf("%w: (%d) > %d)", errTooManyBytes, len(responseBytes), req.BytesLimit)
		}

		var changeProofResp pb.SyncGetChangeProofResponse
		if err := proto.Unmarshal(responseBytes, &changeProofResp); err != nil {
			return nil, err
		}

		// TODO: When the server is updated so that the response can be a
		// RangeProof, this must be updated to handle that case.
		var changeProof merkledb.ChangeProof
		if err := changeProof.UnmarshalProto(changeProofResp.GetChangeProof()); err != nil {
			return nil, err
		}

		// Ensure the response does not contain more than the requested number of leaves
		// and the start and end roots match the requested roots.
		if len(changeProof.KeyChanges) > int(req.KeyLimit) {
			return nil, fmt.Errorf("%w: (%d) > %d)", errTooManyKeys, len(changeProof.KeyChanges), req.KeyLimit)
		}

		endRoot, err := ids.ToID(req.EndRootHash)
		if err != nil {
			return nil, err
		}

		endKey := maybe.Nothing[[]byte]()
		if req.EndKey != nil && !req.EndKey.IsNothing {
			endKey = maybe.Some(req.EndKey.Value)
		}

		if err := db.VerifyChangeProof(ctx, &changeProof, req.StartKey, endKey, endRoot); err != nil {
			return nil, fmt.Errorf("%s due to %w", errInvalidRangeProof, err)
		}
		return &changeProof, nil
	}

	reqBytes, err := proto.Marshal(&pb.Request{
		Message: &pb.Request_ChangeProofRequest{
			ChangeProofRequest: req,
		},
	})
	if err != nil {
		return nil, err
	}
	return getAndParse(ctx, c, reqBytes, parseFn)
}

// GetRangeProof synchronously retrieves the range proof given by [req].
// Upon failure, retries until the context is expired.
// The returned range proof is verified.
func (c *client) GetRangeProof(ctx context.Context, req *pb.SyncGetRangeProofRequest) (*merkledb.RangeProof, error) {
	parseFn := func(ctx context.Context, responseBytes []byte) (*merkledb.RangeProof, error) {
		if len(responseBytes) > int(req.BytesLimit) {
			return nil, fmt.Errorf("%w: (%d) > %d)", errTooManyBytes, len(responseBytes), req.BytesLimit)
		}

		var rangeProofProto pb.RangeProof
		if err := proto.Unmarshal(responseBytes, &rangeProofProto); err != nil {
			return nil, err
		}

		var rangeProof merkledb.RangeProof
		if err := rangeProof.UnmarshalProto(&rangeProofProto); err != nil {
			return nil, err
		}

		// Ensure the response does not contain more than the maximum requested number of leaves.
		if len(rangeProof.KeyValues) > int(req.KeyLimit) {
			return nil, fmt.Errorf("%w: (%d) > %d)", errTooManyKeys, len(rangeProof.KeyValues), req.KeyLimit)
		}

		root, err := ids.ToID(req.RootHash)
		if err != nil {
			return nil, err
		}

		endKey := maybe.Nothing[[]byte]()
		if req.EndKey != nil && !req.EndKey.IsNothing {
			endKey = maybe.Some(req.EndKey.Value)
		}

		if err := rangeProof.Verify(
			ctx,
			req.StartKey,
			endKey,
			root,
		); err != nil {
			return nil, fmt.Errorf("%s due to %w", errInvalidRangeProof, err)
		}
		return &rangeProof, nil
	}

	reqBytes, err := proto.Marshal(&pb.Request{
		Message: &pb.Request_RangeProofRequest{
			RangeProofRequest: req,
		},
	})
	if err != nil {
		return nil, err
	}

	return getAndParse(ctx, c, reqBytes, parseFn)
}

// getAndParse uses [client] to send [request] to an arbitrary peer.
// Returns the response to the request.
// [parseFn] parses the raw response.
// If the request is unsuccessful or the response can't be parsed,
// retries the request to a different peer until [ctx] expires.
// Returns [errAppRequestSendFailed] if we fail to send an AppRequest.
// This should be treated as a fatal error.
func getAndParse[T any](
	ctx context.Context,
	client *client,
	request []byte,
	parseFn func(context.Context, []byte) (*T, error),
) (*T, error) {
	var (
		lastErr  error
		response *T
	)
	// Loop until the context is cancelled or we get a valid response.
	for attempt := 1; ; attempt++ {
		nodeID, responseBytes, err := client.get(ctx, request)
		if err == nil {
			if response, err = parseFn(ctx, responseBytes); err == nil {
				return response, nil
			}
		}

		if errors.Is(err, errAppRequestSendFailed) {
			// Failing to send an AppRequest is a fatal error.
			return nil, err
		}

		client.log.Debug("request failed, retrying",
			zap.Stringer("nodeID", nodeID),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
		// if [err] is being propagated from [ctx], avoid overwriting [lastErr].
		if err != ctx.Err() {
			lastErr = err
		}

		retryWait := initialRetryWait * time.Duration(math.Pow(retryWaitFactor, float64(attempt)))
		if retryWait > maxRetryWait || retryWait < 0 { // Handle overflows with negative check.
			retryWait = maxRetryWait
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				// prefer reporting [lastErr] if it's not nil.
				return nil, fmt.Errorf(
					"request failed after %d attempts with last error %w and ctx error %s",
					attempt, lastErr, ctx.Err(),
				)
			}
			return nil, ctx.Err()
		case <-time.After(retryWait):
		}
	}
}

// get sends [request] to an arbitrary peer and blocks
// until the node receives a response, failure notification
// or [ctx] is canceled.
// Returns the peer's NodeID and response.
// Returns [errAppRequestSendFailed] if we failed to send an AppRequest.
// This should be treated as fatal.
// It's safe to call this method multiple times concurrently.
func (c *client) get(ctx context.Context, request []byte) (ids.NodeID, []byte, error) {
	var (
		response []byte
		nodeID   ids.NodeID
		err      error
	)

	c.metrics.RequestMade()

	if len(c.stateSyncNodes) == 0 {
		nodeID, response, err = c.networkClient.RequestAny(ctx, c.stateSyncMinVersion, request)
	} else {
		// Get the next nodeID to query using the [nodeIdx] offset.
		// If we're out of nodes, loop back to 0.
		// We do this try to query a different node each time if possible.
		nodeIdx := atomic.AddUint32(&c.stateSyncNodeIdx, 1)
		nodeID = c.stateSyncNodes[nodeIdx%uint32(len(c.stateSyncNodes))]
		response, err = c.networkClient.Request(ctx, nodeID, request)
	}
	if err != nil {
		c.metrics.RequestFailed()
		return nodeID, response, err
	}

	c.metrics.RequestSucceeded()
	return nodeID, response, nil
}
