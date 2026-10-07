// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"encoding/json"
	"net/http"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// NewGetAndPostHandler returns a health handler that supports GET and jsonrpc
// POST requests.
func NewGetAndPostHandler(log logging.Logger, reporter Reporter) (http.Handler, error) {
	server, err := rpc.NewHandler(
		"health",
		&Service{
			log:    log,
			health: reporter,
		},
	)
	if err != nil {
		return nil, err
	}

	getHandler := NewGetHandler(reporter.Health)

	// If a GET request is sent, we respond with a 200 if the node is healthy or
	// a 503 if the node isn't healthy.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			server.ServeHTTP(w, r)
			return
		}

		getHandler.ServeHTTP(w, r)
	})
	return handler, nil
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
