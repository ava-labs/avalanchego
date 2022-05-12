// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"net/http"

	"github.com/ava-labs/avalanchego/utils/logging"
)

type Service struct {
	log    logging.Logger
	health Reporter
}

// APIHealthReply is the response for Health
type APIHealthReply struct {
	Checks  map[string]Result `json:"checks"`
	Healthy bool              `json:"healthy"`
}

// Readiness returns if the node has finished initialization
func (s *Service) Readiness(_ *http.Request, _ *struct{}, reply *APIHealthReply) error {
	s.log.Debug("Health.readiness called")
	reply.Checks, reply.Healthy = s.health.Readiness()
	return nil
}

// Health returns a summation of the health of the node
func (s *Service) Health(_ *http.Request, _ *struct{}, reply *APIHealthReply) error {
	s.log.Debug("Health.health called")
	reply.Checks, reply.Healthy = s.health.Health()
	return nil
}

// Liveness returns if the node is in need of a restart
func (s *Service) Liveness(_ *http.Request, _ *struct{}, reply *APIHealthReply) error {
	s.log.Debug("Health.liveness called")
	reply.Checks, reply.Healthy = s.health.Liveness()
	return nil
}
