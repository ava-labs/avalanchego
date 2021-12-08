// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"net/http"
	"time"

	stdjson "encoding/json"

	"github.com/gorilla/rpc/v2"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"

	healthlib "github.com/ava-labs/avalanchego/health"
)

var _ Health = &health{}

// Health wraps a [healthlib.Service]. Handler() returns a handler that handles
// incoming HTTP API requests. We have this in a separate package from
// [healthlib] to avoid a circular import where this service imports
// snow/engine/common but that package imports [healthlib].Checkable
type Health interface {
	healthlib.Service

	Handler() (*common.HTTPHandler, error)
}

func New(checkFreq time.Duration, log logging.Logger, namespace string, registry prometheus.Registerer) (Health, error) {
	service, err := healthlib.NewService(checkFreq, log, namespace, registry)
	return &health{
		Service: service,
		log:     log,
	}, err
}

type health struct {
	healthlib.Service
	log logging.Logger
}

func (h *health) Handler() (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")

	// If a GET request is sent, we respond with a 200 if the node is healthy or
	// a 503 if the node isn't healthy.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			newServer.ServeHTTP(w, r)
			return
		}

		// Make sure the content type is set before writing the header.
		w.Header().Set("Content-Type", "application/json")

		checks, healthy := h.Results()
		if !healthy {
			// If a health check has failed, we should return a 503.
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		// The encoder will call write on the writer, which will write the
		// header with a 200.
		err := stdjson.NewEncoder(w).Encode(APIHealthServerReply{
			Checks:  checks,
			Healthy: healthy,
		})
		if err != nil {
			h.log.Debug("failed to encode the health check response due to %s", err)
		}
	})

	err := newServer.RegisterService(
		&Service{
			log:    h.log,
			health: h.Service,
		},
		"health",
	)
	return &common.HTTPHandler{
		LockOptions: common.NoLock,
		Handler:     handler,
	}, err
}
