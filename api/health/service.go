// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"net/http"
	"time"

	"github.com/AppsFlyer/go-sundheit"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/gorilla/rpc/v2"
)

// defaultCheckOpts is a Check whose properties represent a default Check
var defaultCheckOpts = Check{ExecutionPeriod: time.Minute}

// Health observes a set of vital signs and makes them
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
func (h *Health) RegisterHeartbeat(name string, hb heartbeater, max time.Duration) error {
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

// GetHealthArgs are the arguments for GetHealth
type GetHealthArgs struct{}

// GetHealthReply is the response for GetHealth
type GetHealthReply struct {
	Checks  map[string]health.Result `json:"checks"`
	Healthy bool                     `json:"healthy"`
}

// GetHealth returns a summation of the health of the node
func (service *Health) GetHealth(_ *http.Request, _ *GetHealthArgs, reply *GetHealthReply) error {
	service.log.Debug("Health: GetHealth called")
	reply.Checks, reply.Healthy = service.health.Results()
	return nil
}
