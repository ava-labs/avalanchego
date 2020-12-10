// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"net/http"
	"time"

	health "github.com/AppsFlyer/go-sundheit"
	"github.com/AppsFlyer/go-sundheit/checks"
	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Health observes a set of vital signs and makes them available through an HTTP
// API.
type Health struct {
	log logging.Logger
	// performs the underlying health checks
	health health.Health
}

// CheckRegisterer is an interface that
// can register health checks
type CheckRegisterer interface {
	RegisterCheck(c checks.Check) error
}

// NewService creates a new Health service
func NewService(log logging.Logger) *Health {
	return &Health{log, health.New()}
}

// Handler returns an HTTPHandler providing RPC access to the Health service
func (h *Health) Handler() (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := newServer.RegisterService(h, "health"); err != nil {
		return nil, err
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // GET request --> return 200 if getLiveness returns true, else 503
			if _, healthy := h.health.Results(); healthy {
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

// RegisterHeartbeat adds a check with default options and a CheckFn that checks
// the given heartbeater for a recent heartbeat
func (h *Health) RegisterHeartbeat(name string, hb Heartbeater, max time.Duration) error {
	return h.RegisterCheck(&check{
		name:            name,
		checkFn:         HeartbeatCheckFn(hb, max),
		initialDelay:    constants.DefaultHealthCheckInitialDelay,
		executionPeriod: constants.DefaultHealthCheckExecutionPeriod,
	})
}

// RegisterMonotonicCheckFunc adds a Check with default options and the given CheckFn
// After it passes once, its logic (checkFunc) is never run again; it just passes
func (h *Health) RegisterMonotonicCheckFunc(name string, checkFn func() (interface{}, error)) error {
	check := monotonicCheck{
		check: check{
			name:            name,
			checkFn:         checkFn,
			executionPeriod: constants.DefaultHealthCheckExecutionPeriod,
			initialDelay:    constants.DefaultHealthCheckInitialDelay,
		},
	}
	return h.RegisterCheck(check)
}

// RegisterCheck adds the given Check
func (h *Health) RegisterCheck(c checks.Check) error {
	return h.health.RegisterCheck(&health.Config{
		InitialDelay:    constants.DefaultHealthCheckInitialDelay,
		ExecutionPeriod: constants.DefaultHealthCheckExecutionPeriod,
		Check:           c,
	})
}

// GetLivenessArgs are the arguments for GetLiveness
type GetLivenessArgs struct{}

// GetLivenessReply is the response for GetLiveness
type GetLivenessReply struct {
	Checks  map[string]health.Result `json:"checks"`
	Healthy bool                     `json:"healthy"`
}

// GetLiveness returns a summation of the health of the node
func (h *Health) GetLiveness(_ *http.Request, _ *GetLivenessArgs, reply *GetLivenessReply) error {
	h.log.Info("Health: GetLiveness called")
	reply.Checks, reply.Healthy = h.health.Results()
	return nil
}

type noOp struct{}

// NewNoOpService returns a NoOp version of health check
// for when the Health API is disabled
func NewNoOpService() CheckRegisterer {
	return &noOp{}
}

// RegisterCheck implements the HealthCheckRegisterer interface
func (n *noOp) RegisterCheck(_ checks.Check) error {
	return nil
}
