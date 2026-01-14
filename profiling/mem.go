// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package profiling

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
)

type MemHealthCheck struct{
	LastScanTime time.Time
	Logger logging.Logger
}

func (m *MemHealthCheck) HealthCheck(ctx context.Context) (interface{}, error) {
	if ! m.LastScanTime.IsZero() && time.Since(m.LastScanTime) < 10*time.Minute {
		return nil, nil
	}
	m.LastScanTime = time.Now()

	percent, ok := getAvailableMem(m.Logger)
	if ! ok || percent > 10 {
		return nil, nil
	}

	runtime.GC()
	var allocs bytes.Buffer
	if err := pprof.Lookup("allocs").WriteTo(&allocs, 0); err != nil {
		m.Logger.Warn("Failed writing memory profile", zap.Error(err))
		return nil, err
	}

	var heap bytes.Buffer
	if err := pprof.Lookup("heap").WriteTo(&heap, 0); err != nil {
		m.Logger.Warn("Failed writing memory profile", zap.Error(err))
		return nil, err
	}

	m.Logger.Warn("Low memory available, outputting profile",
		zap.Float64("percent", percent),
		zap.String("allocs", base64.StdEncoding.EncodeToString(allocs.Bytes())))

	m.Logger.Warn("Low memory available, outputting profile",
		zap.Float64("percent", percent),
		zap.String("heap", base64.StdEncoding.EncodeToString(heap.Bytes())))

	return nil, nil
}

func getAvailableMem(logger logging.Logger) (float64, bool){
	client, err := api.NewClient(api.Config{
		Address: "http://localhost:9090",
	})
	if err != nil {
		logger.Warn("Failed creating Prometheus client", zap.Error(err))
		return 0, false
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Query: (Available / Total) * 100
	query := `(node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`

	result, _, err := v1api.Query(ctx, query, time.Now())
	if err != nil {
		logger.Warn("Failed querying Prometheus", zap.Error(err))
		return 0, false
	}
	// Cast the result to a Vector (standard for instant queries)
	vector, ok := result.(model.Vector)
	if !ok || len(vector) == 0 {
		logger.Warn("No data found for the query")
		return 0, false
	}

	// Print the percentage for each instance (host)
	for _, sample := range vector {
		instance := sample.Metric["instance"]
		percent := float64(sample.Value)
		logger.Debug(fmt.Sprintf("Instance %s has %.2f%% available memory", instance, percent))
		return percent, true
	}

	return 0, false
}
