// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	blockBuild  metric.Averager
	blockParse  metric.Averager
	blockVerify metric.Averager
	blockAccept metric.Averager
}

func newMetrics(r *prometheus.Registry) (*Metrics, error) {
	blockBuild, err := metric.NewAverager(
		"snow_block_build",
		"time spent building blocks",
		r,
	)
	if err != nil {
		return nil, err
	}
	blockParse, err := metric.NewAverager(
		"snow_block_parse",
		"time spent parsing blocks",
		r,
	)
	if err != nil {
		return nil, err
	}
	blockVerify, err := metric.NewAverager(
		"snow_block_verify",
		"time spent verifying blocks",
		r,
	)
	if err != nil {
		return nil, err
	}
	blockAccept, err := metric.NewAverager(
		"snow_block_accept",
		"time spent accepting blocks",
		r,
	)
	if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	m := &Metrics{
		blockBuild:  blockBuild,
		blockParse:  blockParse,
		blockVerify: blockVerify,
		blockAccept: blockAccept,
	}

	return m, nil
}
