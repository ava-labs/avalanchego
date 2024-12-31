// Code generated by MockGen. DO NOT EDIT.
// Source: router.go
//
// Generated by this command:
//
//	mockgen -package=routermock -source=router.go -destination=routermock/router.go -mock_names=Router=Router -exclude_interfaces=InternalHandler
//

// Package routermock is a generated GoMock package.
package routermock

import (
	context "context"
	reflect "reflect"
	time "time"

	ids "github.com/ava-labs/avalanchego/ids"
	message "github.com/ava-labs/avalanchego/message"
	p2p "github.com/ava-labs/avalanchego/proto/pb/p2p"
	handler "github.com/ava-labs/avalanchego/snow/networking/handler"
	router "github.com/ava-labs/avalanchego/snow/networking/router"
	timeout "github.com/ava-labs/avalanchego/snow/networking/timeout"
	logging "github.com/ava-labs/avalanchego/utils/logging"
	set "github.com/ava-labs/avalanchego/utils/set"
	version "github.com/ava-labs/avalanchego/version"
	prometheus "github.com/prometheus/client_golang/prometheus"
	gomock "go.uber.org/mock/gomock"
)

// Router is a mock of Router interface.
type Router struct {
	ctrl     *gomock.Controller
	recorder *RouterMockRecorder
}

// RouterMockRecorder is the mock recorder for Router.
type RouterMockRecorder struct {
	mock *Router
}

// NewRouter creates a new mock instance.
func NewRouter(ctrl *gomock.Controller) *Router {
	mock := &Router{ctrl: ctrl}
	mock.recorder = &RouterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Router) EXPECT() *RouterMockRecorder {
	return m.recorder
}

// AddChain mocks base method.
func (m *Router) AddChain(ctx context.Context, chain handler.Handler) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddChain", ctx, chain)
}

// AddChain indicates an expected call of AddChain.
func (mr *RouterMockRecorder) AddChain(ctx, chain any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddChain", reflect.TypeOf((*Router)(nil).AddChain), ctx, chain)
}

// Benched mocks base method.
func (m *Router) Benched(chainID ids.ID, validatorID ids.NodeID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Benched", chainID, validatorID)
}

// Benched indicates an expected call of Benched.
func (mr *RouterMockRecorder) Benched(chainID, validatorID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Benched", reflect.TypeOf((*Router)(nil).Benched), chainID, validatorID)
}

// Connected mocks base method.
func (m *Router) Connected(nodeID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Connected", nodeID, nodeVersion, subnetID)
}

// Connected indicates an expected call of Connected.
func (mr *RouterMockRecorder) Connected(nodeID, nodeVersion, subnetID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Connected", reflect.TypeOf((*Router)(nil).Connected), nodeID, nodeVersion, subnetID)
}

// Disconnected mocks base method.
func (m *Router) Disconnected(nodeID ids.NodeID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Disconnected", nodeID)
}

// Disconnected indicates an expected call of Disconnected.
func (mr *RouterMockRecorder) Disconnected(nodeID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Disconnected", reflect.TypeOf((*Router)(nil).Disconnected), nodeID)
}

// HandleInbound mocks base method.
func (m *Router) HandleInbound(arg0 context.Context, arg1 message.InboundMessage) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "HandleInbound", arg0, arg1)
}

// HandleInbound indicates an expected call of HandleInbound.
func (mr *RouterMockRecorder) HandleInbound(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandleInbound", reflect.TypeOf((*Router)(nil).HandleInbound), arg0, arg1)
}

// HealthCheck mocks base method.
func (m *Router) HealthCheck(arg0 context.Context) (any, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HealthCheck", arg0)
	ret0, _ := ret[0].(any)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// HealthCheck indicates an expected call of HealthCheck.
func (mr *RouterMockRecorder) HealthCheck(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HealthCheck", reflect.TypeOf((*Router)(nil).HealthCheck), arg0)
}

// Initialize mocks base method.
func (m *Router) Initialize(nodeID ids.NodeID, log logging.Logger, timeouts timeout.Manager, shutdownTimeout time.Duration, criticalChains set.Set[ids.ID], sybilProtectionEnabled bool, trackedSubnets set.Set[ids.ID], onFatal func(int), healthConfig router.HealthConfig, reg prometheus.Registerer) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Initialize", nodeID, log, timeouts, shutdownTimeout, criticalChains, sybilProtectionEnabled, trackedSubnets, onFatal, healthConfig, reg)
	ret0, _ := ret[0].(error)
	return ret0
}

// Initialize indicates an expected call of Initialize.
func (mr *RouterMockRecorder) Initialize(nodeID, log, timeouts, shutdownTimeout, criticalChains, sybilProtectionEnabled, trackedSubnets, onFatal, healthConfig, reg any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Initialize", reflect.TypeOf((*Router)(nil).Initialize), nodeID, log, timeouts, shutdownTimeout, criticalChains, sybilProtectionEnabled, trackedSubnets, onFatal, healthConfig, reg)
}

// RegisterRequest mocks base method.
func (m *Router) RegisterRequest(ctx context.Context, nodeID ids.NodeID, chainID ids.ID, requestID uint32, op message.Op, failedMsg message.InboundMessage, engineType p2p.EngineType) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RegisterRequest", ctx, nodeID, chainID, requestID, op, failedMsg, engineType)
}

// RegisterRequest indicates an expected call of RegisterRequest.
func (mr *RouterMockRecorder) RegisterRequest(ctx, nodeID, chainID, requestID, op, failedMsg, engineType any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterRequest", reflect.TypeOf((*Router)(nil).RegisterRequest), ctx, nodeID, chainID, requestID, op, failedMsg, engineType)
}

// Shutdown mocks base method.
func (m *Router) Shutdown(arg0 context.Context) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Shutdown", arg0)
}

// Shutdown indicates an expected call of Shutdown.
func (mr *RouterMockRecorder) Shutdown(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Shutdown", reflect.TypeOf((*Router)(nil).Shutdown), arg0)
}

// Unbenched mocks base method.
func (m *Router) Unbenched(chainID ids.ID, validatorID ids.NodeID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Unbenched", chainID, validatorID)
}

// Unbenched indicates an expected call of Unbenched.
func (mr *RouterMockRecorder) Unbenched(chainID, validatorID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Unbenched", reflect.TypeOf((*Router)(nil).Unbenched), chainID, validatorID)
}
