// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extension

import (
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers"
)

type LeafRequestConfig struct {
	// LeafType is the type of the leaf node
	LeafType message.NodeType
	// MetricName is the name of the metric to use for the leaf request
	MetricName string
	// Handler is the handler to use for the leaf request
	Handler handlers.LeafRequestHandler
}
