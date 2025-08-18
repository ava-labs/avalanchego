// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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

// APIArgs is the arguments for Readiness, Health, and Liveness.
type APIArgs struct {
	Tags []string `json:"tags"`
}

// Readiness returns if the node has finished initialization
func (s *Service) Readiness(_ *http.Request, args *APIArgs, reply *APIReply) error {
	s.log.Debug("API called",
		zap.String("service", "health"),
		zap.String("method", "readiness"),
		zap.Strings("tags", args.Tags),
	)
	reply.Checks, reply.Healthy = s.health.Readiness(args.Tags...)
	return nil
}

// Health returns a summation of the health of the node
func (s *Service) Health(_ *http.Request, args *APIArgs, reply *APIReply) error {
	s.log.Debug("API called",
		zap.String("service", "health"),
		zap.String("method", "health"),
		zap.Strings("tags", args.Tags),
	)

	reply.Checks, reply.Healthy = s.health.Health(args.Tags...)
	return nil
}

// Liveness returns if the node is in need of a restart
func (s *Service) Liveness(_ *http.Request, args *APIArgs, reply *APIReply) error {
	s.log.Debug("API called",
		zap.String("service", "health"),
		zap.String("method", "liveness"),
		zap.Strings("tags", args.Tags),
	)
	reply.Checks, reply.Healthy = s.health.Liveness(args.Tags...)
	return nil
}
