// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

type Service struct {
	log    logging.Logger
	health Reporter
}

// APIReply is the response for Readiness, Health, and Liveness.
type APIReply struct {
	Checks  map[string]Result `json:"checks"`
	Healthy bool              `json:"healthy"`
}

// Readiness returns if the node has finished initialization
func (s *Service) Readiness(_ *http.Request, _ *struct{}, reply *APIReply) error {
	s.log.Debug("API called",
		zap.String("service", "health"),
		zap.String("method", "readiness"),
	)
	reply.Checks, reply.Healthy = s.health.Readiness()
	return nil
}

// Health returns a summation of the health of the node
func (s *Service) Health(_ *http.Request, _ *struct{}, reply *APIReply) error {
	s.log.Debug("API called",
		zap.String("service", "health"),
		zap.String("method", "health"),
	)
	reply.Checks, reply.Healthy = s.health.Health()
	return nil
}

// Liveness returns if the node is in need of a restart
func (s *Service) Liveness(_ *http.Request, _ *struct{}, reply *APIReply) error {
	s.log.Debug("API called",
		zap.String("service", "health"),
		zap.String("method", "liveness"),
	)
	reply.Checks, reply.Healthy = s.health.Liveness()
	return nil
}
