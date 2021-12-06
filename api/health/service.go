// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"net/http"

	stdjson "encoding/json"

	healthback "github.com/AppsFlyer/go-sundheit"

	"github.com/ava-labs/avalanchego/utils/logging"

	healthlib "github.com/ava-labs/avalanchego/health"
)

type Service struct {
	log    logging.Logger
	health healthlib.Service
}

// APIHealthArgs are the arguments for Health
type APIHealthArgs struct{}

// APIHealthReply is the response for Health
type APIHealthServerReply struct {
	Checks  map[string]healthback.Result `json:"checks"`
	Healthy bool                         `json:"healthy"`
}

// Health returns a summation of the health of the node
func (s *Service) Health(_ *http.Request, _ *APIHealthArgs, reply *APIHealthServerReply) error {
	s.log.Debug("Health.health called")
	reply.Checks, reply.Healthy = s.health.Results()
	if reply.Healthy {
		return nil
	}
	replyStr, err := stdjson.Marshal(reply.Checks)
	s.log.Warn("Health.health is returning an error: %s", string(replyStr))
	return err
}
