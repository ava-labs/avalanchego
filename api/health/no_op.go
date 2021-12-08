// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	healthback "github.com/AppsFlyer/go-sundheit"

	"github.com/ava-labs/avalanchego/snow/engine/common"

	healthlib "github.com/ava-labs/avalanchego/health"
)

var _ Health = &noOp{}

type noOp struct{}

// NewNoOp returns a noop version of the health interface that does nothing for
// when the Health API is disabled
func NewNoOp() Health { return &noOp{} }

func (n *noOp) Results() (map[string]healthback.Result, bool) {
	return map[string]healthback.Result{}, true
}

func (n *noOp) RegisterCheck(string, healthlib.Check) error { return nil }

func (n *noOp) RegisterMonotonicCheck(string, healthlib.Check) error { return nil }

func (n *noOp) Handler() (*common.HTTPHandler, error) { return nil, nil }
