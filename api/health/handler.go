// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/utils/logging"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

// NewGetAndPostHandler returns a health handler that supports GET and jsonrpc
// POST requests.
func NewGetAndPostHandler(log logging.Logger, reporter Reporter) (http.Handler, error) {
	newServer := rpc.NewServer()
	codec := avajson.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")

	getHandler := NewGetHandler(reporter.Health)

	// If a GET request is sent, we respond with a 200 if the node is healthy or
	// a 503 if the node isn't healthy.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			newServer.ServeHTTP(w, r)
			return
		}

		getHandler.ServeHTTP(w, r)
	})

	err := newServer.RegisterService(
		&Service{
			log:    log,
			health: reporter,
		},
		"health",
	)
	return handler, err
}

// NewGetHandler return a health handler that supports GET requests reporting
// the result of the provided [reporter].
func NewGetHandler(reporter func(tags ...string) (map[string]Result, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Make sure the content type is set before writing the header.
		w.Header().Set("Content-Type", "application/json")

		tags := r.URL.Query()["tag"]
		checks, healthy := reporter(tags...)
		if !healthy {
			// If a health check has failed, we should return a 503.
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		// The encoder will call write on the writer, which will write the
		// header with a 200.
		_ = json.NewEncoder(w).Encode(APIReply{
			Checks:  checks,
			Healthy: healthy,
		})
	})
}
