// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/snow/networking/tracker (interfaces: Targeter)
//
// Generated by this command:
//
//	mockgen -package=trackermock -destination=trackermock/targeter.go -mock_names=Targeter=Targeter . Targeter
//

// Package trackermock is a generated GoMock package.
package trackermock

import (
	reflect "reflect"

	ids "github.com/ava-labs/avalanchego/ids"
	gomock "go.uber.org/mock/gomock"
)

// Targeter is a mock of Targeter interface.
type Targeter struct {
	ctrl     *gomock.Controller
	recorder *TargeterMockRecorder
	isgomock struct{}
}

// TargeterMockRecorder is the mock recorder for Targeter.
type TargeterMockRecorder struct {
	mock *Targeter
}

// NewTargeter creates a new mock instance.
func NewTargeter(ctrl *gomock.Controller) *Targeter {
	mock := &Targeter{ctrl: ctrl}
	mock.recorder = &TargeterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Targeter) EXPECT() *TargeterMockRecorder {
	return m.recorder
}

// TargetUsage mocks base method.
func (m *Targeter) TargetUsage(nodeID ids.NodeID) float64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "TargetUsage", nodeID)
	ret0, _ := ret[0].(float64)
	return ret0
}

// TargetUsage indicates an expected call of TargetUsage.
func (mr *TargeterMockRecorder) TargetUsage(nodeID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "TargetUsage", reflect.TypeOf((*Targeter)(nil).TargetUsage), nodeID)
}
