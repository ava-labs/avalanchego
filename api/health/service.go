// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"net/http"
	"time"

	health "github.com/AppsFlyer/go-sundheit"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanche-go/snow/engine/common"
	"github.com/ava-labs/avalanche-go/utils/json"
	"github.com/ava-labs/avalanche-go/utils/logging"
)

// defaultCheckOpts is a Check whose properties represent a default Check
var defaultCheckOpts = check{
	executionPeriod: time.Minute,
	initialDelay:    10 * time.Second,
}

// Health observes a set of vital signs and makes them available through an HTTP
// API.
type Health struct {
	log    logging.Logger
	health health.Health
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
	return h.RegisterCheckFunc(name, HeartbeatCheckFn(hb, max))
}

// RegisterCheckFunc adds a Check with default options and the given CheckFn
func (h *Health) RegisterCheckFunc(name string, checkFn CheckFn) error {
	check := defaultCheckOpts
	check.name = name
	check.checkFn = checkFn
	return h.RegisterCheck(check)
}

// RegisterMonotonicCheckFunc adds a Check with default options and the given CheckFn
// After it passes once, its logic (checkFunc) is never run again; it just passes
func (h *Health) RegisterMonotonicCheckFunc(name string, checkFn CheckFn) error {
	check := monotonicCheck{check: defaultCheckOpts}
	check.name = name
	check.checkFn = checkFn
	return h.RegisterCheck(check)
}

// RegisterCheck adds the given Check
func (h *Health) RegisterCheck(c Check) error {
	return h.health.RegisterCheck(&health.Config{
		InitialDelay:     c.InitialDelay(),
		ExecutionPeriod:  c.ExecutionPeriod(),
		InitiallyPassing: c.InitiallyPassing(),
		Check:            c,
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
