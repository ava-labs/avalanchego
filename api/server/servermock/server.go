// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/api/server (interfaces: Server)
//
// Generated by this command:
//
//	mockgen -package=servermock -destination=servermock/server.go -mock_names=Server=Server . Server
//

// Package servermock is a generated GoMock package.
package servermock

import (
	http "net/http"
	reflect "reflect"

	snow "github.com/ava-labs/avalanchego/snow"
	common "github.com/ava-labs/avalanchego/snow/engine/common"
	gomock "go.uber.org/mock/gomock"
)

// Server is a mock of Server interface.
type Server struct {
	ctrl     *gomock.Controller
	recorder *ServerMockRecorder
}

// ServerMockRecorder is the mock recorder for Server.
type ServerMockRecorder struct {
	mock *Server
}

// NewServer creates a new mock instance.
func NewServer(ctrl *gomock.Controller) *Server {
	mock := &Server{ctrl: ctrl}
	mock.recorder = &ServerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Server) EXPECT() *ServerMockRecorder {
	return m.recorder
}

// AddAliases mocks base method.
func (m *Server) AddAliases(arg0 string, arg1 ...string) error {
	m.ctrl.T.Helper()
	varargs := []any{arg0}
	for _, a := range arg1 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "AddAliases", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddAliases indicates an expected call of AddAliases.
func (mr *ServerMockRecorder) AddAliases(arg0 any, arg1 ...any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{arg0}, arg1...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddAliases", reflect.TypeOf((*Server)(nil).AddAliases), varargs...)
}

// AddAliasesWithReadLock mocks base method.
func (m *Server) AddAliasesWithReadLock(arg0 string, arg1 ...string) error {
	m.ctrl.T.Helper()
	varargs := []any{arg0}
	for _, a := range arg1 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "AddAliasesWithReadLock", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddAliasesWithReadLock indicates an expected call of AddAliasesWithReadLock.
func (mr *ServerMockRecorder) AddAliasesWithReadLock(arg0 any, arg1 ...any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{arg0}, arg1...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddAliasesWithReadLock", reflect.TypeOf((*Server)(nil).AddAliasesWithReadLock), varargs...)
}

// AddRoute mocks base method.
func (m *Server) AddRoute(arg0 http.Handler, arg1, arg2 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddRoute", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddRoute indicates an expected call of AddRoute.
func (mr *ServerMockRecorder) AddRoute(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddRoute", reflect.TypeOf((*Server)(nil).AddRoute), arg0, arg1, arg2)
}

// AddRouteWithReadLock mocks base method.
func (m *Server) AddRouteWithReadLock(arg0 http.Handler, arg1, arg2 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddRouteWithReadLock", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddRouteWithReadLock indicates an expected call of AddRouteWithReadLock.
func (mr *ServerMockRecorder) AddRouteWithReadLock(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddRouteWithReadLock", reflect.TypeOf((*Server)(nil).AddRouteWithReadLock), arg0, arg1, arg2)
}

// Dispatch mocks base method.
func (m *Server) Dispatch() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Dispatch")
	ret0, _ := ret[0].(error)
	return ret0
}

// Dispatch indicates an expected call of Dispatch.
func (mr *ServerMockRecorder) Dispatch() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Dispatch", reflect.TypeOf((*Server)(nil).Dispatch))
}

// RegisterChain mocks base method.
func (m *Server) RegisterChain(arg0 string, arg1 *snow.ConsensusContext, arg2 common.VM) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RegisterChain", arg0, arg1, arg2)
}

// RegisterChain indicates an expected call of RegisterChain.
func (mr *ServerMockRecorder) RegisterChain(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterChain", reflect.TypeOf((*Server)(nil).RegisterChain), arg0, arg1, arg2)
}

// Shutdown mocks base method.
func (m *Server) Shutdown() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Shutdown")
	ret0, _ := ret[0].(error)
	return ret0
}

// Shutdown indicates an expected call of Shutdown.
func (mr *ServerMockRecorder) Shutdown() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Shutdown", reflect.TypeOf((*Server)(nil).Shutdown))
}
