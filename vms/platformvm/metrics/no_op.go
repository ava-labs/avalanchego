// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"net/http"
	"time"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
)

var _ Metrics = &noopMetrics{}

type noopMetrics struct{}

func NewNoopMetrics() *noopMetrics {
	return &noopMetrics{}
}

func (m *noopMetrics) MarkOptionVoteWon() {}

func (m *noopMetrics) MarkOptionVoteLost() {}

func (m *noopMetrics) MarkAccepted(blocks.Block) error { return nil }

func (m *noopMetrics) InterceptRequestFunc() func(*rpc.RequestInfo) *http.Request {
	return func(*rpc.RequestInfo) *http.Request { return nil }
}

func (m *noopMetrics) AfterRequestFunc() func(*rpc.RequestInfo) {
	return func(ri *rpc.RequestInfo) {}
}

func (m *noopMetrics) IncValidatorSetsCreated() {}

func (m *noopMetrics) IncValidatorSetsCached() {}

func (m *noopMetrics) AddValidatorSetsDuration(time.Duration) {}

func (m *noopMetrics) AddValidatorSetsHeightDiff(float64) {}

func (m *noopMetrics) SetLocalStake(float64) {}

func (m *noopMetrics) SetTotalStake(float64) {}

func (m *noopMetrics) SetSubnetPercentConnected(ids.ID, float64) {}

func (m *noopMetrics) SetPercentConnected(float64) {}
