// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connecthandler

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/proto/pb/health/v1/healthv1connect"

	healthv1 "github.com/ava-labs/avalanchego/proto/pb/health/v1"
)

var _ healthv1connect.HealthServiceHandler = (*ConnectHealthService)(nil)

type ConnectHealthService struct {
	health health.Health
}

// NewConnectHealthService returns a ConnectRPC handler for the Health API
func NewConnectHealthService(health health.Health) *ConnectHealthService {
	return &ConnectHealthService{health: health}
}

func (c *ConnectHealthService) Readiness(
	_ context.Context,
	request *connect.Request[healthv1.ReadinessRequest],
) (*connect.Response[healthv1.ReadinessResponse], error) {
	checks, healthy := c.health.Readiness(request.Msg.Tags...)

	checksProto := convertResults(checks)

	out := &healthv1.ReadinessResponse{
		Checks:  checksProto,
		Healthy: healthy,
	}

	return connect.NewResponse(out), nil
}

func (c *ConnectHealthService) Health(
	_ context.Context,
	request *connect.Request[healthv1.HealthRequest],
) (*connect.Response[healthv1.HealthResponse], error) {
	checks, healthy := c.health.Health(request.Msg.Tags...)

	checksProto := convertResults(checks)

	out := &healthv1.HealthResponse{
		Checks:  checksProto,
		Healthy: healthy,
	}

	return connect.NewResponse(out), nil
}

func (c *ConnectHealthService) Liveness(
	_ context.Context,
	request *connect.Request[healthv1.LivenessRequest],
) (*connect.Response[healthv1.LivenessResponse], error) {
	checks, healthy := c.health.Liveness(request.Msg.Tags...)

	checksProto := convertResults(checks)

	out := &healthv1.LivenessResponse{
		Checks:  checksProto,
		Healthy: healthy,
	}

	return connect.NewResponse(out), nil
}

// convertResult transforms a health.Result into a healthv1.Result
func convertResult(r health.Result) *healthv1.Result {
	result := &healthv1.Result{
		ContiguousFailures: r.ContiguousFailures,
		DurationNs:         r.Duration.Nanoseconds(),
	}

	// Handle message field (from the Details field)
	if r.Details != nil {
		if m, ok := r.Details.(map[string]interface{}); ok {
			if structMsg, err := structpb.NewStruct(m); err == nil {
				result.Message = structMsg
			}
		} else if msg, ok := r.Details.(string); ok {
			// Convert string to a Struct with a single field "value"
			result.Message, _ = structpb.NewStruct(map[string]interface{}{
				"value": msg,
			})
		}
	}

	if r.Error != nil {
		result.Error = *r.Error
	}

	if !r.Timestamp.IsZero() {
		result.Timestamp = timestamppb.New(r.Timestamp)
	}

	// Set time of first failure if exists and not nil
	if r.TimeOfFirstFailure != nil {
		result.TimeOfFirstFailure = timestamppb.New(*r.TimeOfFirstFailure)
	} else {
		// Use zero time if TimeOfFirstFailure is nil
		result.TimeOfFirstFailure = timestamppb.New(time.Time{})
	}

	return result
}

// convertResults transforms a map of health.Result into a map of *healthv1.Result
func convertResults(results map[string]health.Result) map[string]*healthv1.Result {
	converted := make(map[string]*healthv1.Result)
	for name, res := range results {
		converted[name] = convertResult(res)
	}
	return converted
}
