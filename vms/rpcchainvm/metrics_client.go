// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"

	"google.golang.org/protobuf/types/known/emptypb"
)

var _ prometheus.Gatherer = &VMClient{}

func (vm *VMClient) Gather() ([]*dto.MetricFamily, error) {
	resp, err := vm.client.Gather(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.MetricFamilies, nil
}
