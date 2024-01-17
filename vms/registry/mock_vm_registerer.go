// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/vms/registry (interfaces: VMRegisterer)
//
// Generated by this command:
//
//	mockgen -package=registry -destination=vms/registry/mock_vm_registerer.go github.com/ava-labs/avalanchego/vms/registry VMRegisterer
//

// Package registry is a generated GoMock package.
package registry

import (
	context "context"
	reflect "reflect"

	ids "github.com/ava-labs/avalanchego/ids"
	vms "github.com/ava-labs/avalanchego/vms"
	gomock "go.uber.org/mock/gomock"
)

// MockVMRegisterer is a mock of VMRegisterer interface.
type MockVMRegisterer struct {
	ctrl     *gomock.Controller
	recorder *MockVMRegistererMockRecorder
}

// MockVMRegistererMockRecorder is the mock recorder for MockVMRegisterer.
type MockVMRegistererMockRecorder struct {
	mock *MockVMRegisterer
}

// NewMockVMRegisterer creates a new mock instance.
func NewMockVMRegisterer(ctrl *gomock.Controller) *MockVMRegisterer {
	mock := &MockVMRegisterer{ctrl: ctrl}
	mock.recorder = &MockVMRegistererMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockVMRegisterer) EXPECT() *MockVMRegistererMockRecorder {
	return m.recorder
}

// Register mocks base method.
func (m *MockVMRegisterer) Register(arg0 context.Context, arg1 ids.ID, arg2 vms.Factory) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Register", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// Register indicates an expected call of Register.
func (mr *MockVMRegistererMockRecorder) Register(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Register", reflect.TypeOf((*MockVMRegisterer)(nil).Register), arg0, arg1, arg2)
}

// RegisterWithReadLock mocks base method.
func (m *MockVMRegisterer) RegisterWithReadLock(arg0 context.Context, arg1 ids.ID, arg2 vms.Factory) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterWithReadLock", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// RegisterWithReadLock indicates an expected call of RegisterWithReadLock.
func (mr *MockVMRegistererMockRecorder) RegisterWithReadLock(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterWithReadLock", reflect.TypeOf((*MockVMRegisterer)(nil).RegisterWithReadLock), arg0, arg1, arg2)
}
