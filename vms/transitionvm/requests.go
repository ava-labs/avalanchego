// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ common.AppSender = (*sender)(nil)

//nolint:unused // False positive
type request struct {
	nodeID    ids.NodeID
	requestID uint32
}

type requests struct {
	lock sync.Mutex
	set  set.Set[request]
}

func (r *requests) add(nodeIDs set.Set[ids.NodeID], requestID uint32) {
	r.lock.Lock()
	defer r.lock.Unlock()

	for nodeID := range nodeIDs {
		r.set.Add(request{nodeID, requestID})
	}
}

func (r *requests) remove(nodeID ids.NodeID, requestID uint32) bool {
	r.lock.Lock()
	defer r.lock.Unlock()

	req := request{nodeID, requestID}
	had := r.set.Contains(req)
	r.set.Remove(req)
	return had
}

type sender struct {
	common.AppSender

	requests *requests
}

func (s *sender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	s.requests.add(nodeIDs, requestID)
	return s.AppSender.SendAppRequest(ctx, nodeIDs, requestID, appRequestBytes)
}
