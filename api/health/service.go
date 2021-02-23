// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"net/http"
	"time"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/health"
	healthlib "github.com/ava-labs/avalanchego/health"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Service wraps a [healthlib.Service]. Handler() returns a handler
// that handles incoming HTTP API requests. We have this in a separate
// package from [healthlib] to avoid a circular import where this service
// imports snow/engine/common but that package imports [healthlib].Checkable
type Service interface {
	healthlib.Service
	Handler() (*common.HTTPHandler, error)
}

func NewService(checkFreq time.Duration, log logging.Logger) Service {
	return &apiServer{
		Service: healthlib.NewService(checkFreq),
		log:     log,
	}
}

// APIServer serves HTTP for a health service
type apiServer struct {
	healthlib.Service
	log logging.Logger
}

func (as *apiServer) Handler() (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := newServer.RegisterService(as, "health"); err != nil {
		return nil, err
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // GET request --> return 200 if getLiveness returns true, else 503
			if _, healthy := as.Results(); healthy {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		} else {
			newServer.ServeHTTP(w, r) // Other request --> use JSON RPC
		}
	})
	return &common.HTTPHandler{LockOptions: common.NoLock, Handler: handler}, nil
}

// APIHealthArgs are the arguments for Health
type APIHealthArgs struct{}

// APIHealthReply is the response for Health
type APIHealthReply struct {
	Checks  map[string]interface{} `json:"checks"`
	Healthy bool                   `json:"healthy"`
}

// Health returns a summation of the health of the node
func (as *apiServer) Health(_ *http.Request, _ *APIHealthArgs, reply *APIHealthReply) error {
	as.log.Info("Health.health called")
	reply.Checks, reply.Healthy = as.Results()
	return nil
}

// GetLiveness returns a summation of the health of the node
// Deprecated in favor of Health
func (as *apiServer) GetLiveness(_ *http.Request, _ *APIHealthArgs, reply *APIHealthReply) error {
	as.log.Info("Health: GetLiveness called")
	reply.Checks, reply.Healthy = as.Results()
	return nil
}

type noOp struct{}

// NewNoOpService returns a NoOp version of health check
// for when the Health API is disabled
func NewNoOpService() Service {
	return &noOp{}
}

// RegisterCheck implements the Service interface
func (n *noOp) Results() (map[string]interface{}, bool) {
	return nil, true
}

// RegisterCheck implements the Service interface
func (n *noOp) Handler() (_ *common.HTTPHandler, _ error) {
	return nil, nil
}

// // RegisterCheck implements the Service interface
// func (n *noOp) RegisterCheck(_ string, _ healthlib.Checkable) error {
// 	return nil
// }

// RegisterCheckFn implements the Service interface
func (n *noOp) RegisterCheck(_ string, _ health.Check) error {
	return nil
}

// RegisterMonotonicCheckFn implements the Service interface
func (n *noOp) RegisterMonotonicCheck(_ string, _ health.Check) error {
	return nil
}
