// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/vms/platformvm/fx (interfaces: Fx)
//
// Generated by this command:
//
//	mockgen -package=fxmock -destination=vms/platformvm/fx/fxmock/fx.go -mock_names=Fx=Fx github.com/ava-labs/avalanchego/vms/platformvm/fx Fx
//

// Package fxmock is a generated GoMock package.
package fxmock

import (
	reflect "reflect"

	gomock "go.uber.org/mock/gomock"
)

// Fx is a mock of Fx interface.
type Fx struct {
	ctrl     *gomock.Controller
	recorder *FxMockRecorder
}

// FxMockRecorder is the mock recorder for Fx.
type FxMockRecorder struct {
	mock *Fx
}

// NewFx creates a new mock instance.
func NewFx(ctrl *gomock.Controller) *Fx {
	mock := &Fx{ctrl: ctrl}
	mock.recorder = &FxMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Fx) EXPECT() *FxMockRecorder {
	return m.recorder
}

// Bootstrapped mocks base method.
func (m *Fx) Bootstrapped() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Bootstrapped")
	ret0, _ := ret[0].(error)
	return ret0
}

// Bootstrapped indicates an expected call of Bootstrapped.
func (mr *FxMockRecorder) Bootstrapped() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Bootstrapped", reflect.TypeOf((*Fx)(nil).Bootstrapped))
}

// Bootstrapping mocks base method.
func (m *Fx) Bootstrapping() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Bootstrapping")
	ret0, _ := ret[0].(error)
	return ret0
}

// Bootstrapping indicates an expected call of Bootstrapping.
func (mr *FxMockRecorder) Bootstrapping() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Bootstrapping", reflect.TypeOf((*Fx)(nil).Bootstrapping))
}

// CreateOutput mocks base method.
func (m *Fx) CreateOutput(arg0 uint64, arg1 any) (any, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateOutput", arg0, arg1)
	ret0, _ := ret[0].(any)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateOutput indicates an expected call of CreateOutput.
func (mr *FxMockRecorder) CreateOutput(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateOutput", reflect.TypeOf((*Fx)(nil).CreateOutput), arg0, arg1)
}

// Initialize mocks base method.
func (m *Fx) Initialize(arg0 any) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Initialize", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Initialize indicates an expected call of Initialize.
func (mr *FxMockRecorder) Initialize(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Initialize", reflect.TypeOf((*Fx)(nil).Initialize), arg0)
}

// VerifyPermission mocks base method.
func (m *Fx) VerifyPermission(arg0, arg1, arg2, arg3 any) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VerifyPermission", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// VerifyPermission indicates an expected call of VerifyPermission.
func (mr *FxMockRecorder) VerifyPermission(arg0, arg1, arg2, arg3 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VerifyPermission", reflect.TypeOf((*Fx)(nil).VerifyPermission), arg0, arg1, arg2, arg3)
}

// VerifyTransfer mocks base method.
func (m *Fx) VerifyTransfer(arg0, arg1, arg2, arg3 any) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VerifyTransfer", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// VerifyTransfer indicates an expected call of VerifyTransfer.
func (mr *FxMockRecorder) VerifyTransfer(arg0, arg1, arg2, arg3 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VerifyTransfer", reflect.TypeOf((*Fx)(nil).VerifyTransfer), arg0, arg1, arg2, arg3)
}
