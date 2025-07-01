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

	healthv1 "github.com/ava-labs/avalanchego/proto/pb/health/v1"
)

// NewConnectHealthService returns a ConnectRPC-compatible ConnectHealthService
// that delegates calls to the existing health service implementation.
func NewConnectHealthService(healthService health.Health) *ConnectHealthService {
	return &ConnectHealthService{
		Service: healthService,
	}
}

type ConnectHealthService struct {
	Service health.Health
}

func (s *ConnectHealthService) Readiness(
	_ context.Context,
	req *connect.Request[healthv1.ReadinessArgs],
) (*connect.Response[healthv1.ReadinessReply], error) {
	// The health.Health interface has different methods than the service.
	// We need to use the methods of the Health interface.
	checks, healthy := s.Service.Readiness(req.Msg.Tags...)

	// Convert the health.Result map to protobuf format
	checksProto := make(map[string]*healthv1.Result, len(checks))
	for name, check := range checks {
		checksProto[name] = convertResult(check)
	}

	out := &healthv1.ReadinessReply{
		Checks:  checksProto,
		Healthy: healthy,
	}

	return connect.NewResponse(out), nil
}

func (s *ConnectHealthService) Health(
	_ context.Context,
	req *connect.Request[healthv1.HealthArgs],
) (*connect.Response[healthv1.HealthReply], error) {
	// The health.Health interface has different methods than the service.
	// We need to use the methods of the Health interface.
	checks, healthy := s.Service.Health(req.Msg.Tags...)

	// Convert the health.Result map to protobuf format
	checksProto := make(map[string]*healthv1.Result, len(checks))
	for name, check := range checks {
		checksProto[name] = convertResult(check)
	}

	out := &healthv1.HealthReply{
		Checks:  checksProto,
		Healthy: healthy,
	}

	return connect.NewResponse(out), nil
}

func (s *ConnectHealthService) Liveness(
	_ context.Context,
	req *connect.Request[healthv1.LivenessArgs],
) (*connect.Response[healthv1.LivenessReply], error) {
	// The health.Health interface has different methods than the service.
	// We need to use the methods of the Health interface.
	checks, healthy := s.Service.Liveness(req.Msg.Tags...)

	// Convert the health.Result map to protobuf format
	checksProto := make(map[string]*healthv1.Result, len(checks))
	for name, check := range checks {
		checksProto[name] = convertResult(check)
	}

	out := &healthv1.LivenessReply{
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

	// Set error field if exists
	if r.Error != nil {
		result.Error = *r.Error
	}

	// Set timestamp if not zero
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
