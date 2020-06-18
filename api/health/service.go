// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"net/http"
	"time"

	health "github.com/AppsFlyer/go-sundheit"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/logging"
)

// defaultCheckOpts is a Check whose properties represent a default Check
var defaultCheckOpts = Check{ExecutionPeriod: time.Minute}

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
func (h *Health) Handler() *common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	newServer.RegisterService(h, "health")
	return &common.HTTPHandler{LockOptions: common.NoLock, Handler: newServer}
}

// RegisterHeartbeat adds a check with default options and a CheckFn that checks
// the given heartbeater for a recent heartbeat
func (h *Health) RegisterHeartbeat(name string, hb Heartbeater, max time.Duration) error {
	return h.RegisterCheckFunc(name, HeartbeatCheckFn(hb, max))
}

// RegisterCheckFunc adds a Check with default options and the given CheckFn
func (h *Health) RegisterCheckFunc(name string, checkFn CheckFn) error {
	check := defaultCheckOpts
	check.Name = name
	check.CheckFn = checkFn
	return h.RegisterCheck(check)
}

// RegisterCheck adds the given Check
func (h *Health) RegisterCheck(c Check) error {
	return h.health.RegisterCheck(&health.Config{
		InitialDelay:     c.InitialDelay,
		ExecutionPeriod:  c.ExecutionPeriod,
		InitiallyPassing: c.InitiallyPassing,
		Check:            gosundheitCheck{c.Name, c.CheckFn},
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
