// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/codec (interfaces: Manager)
//
// Generated by this command:
//
//	mockgen -package=codecmock -destination=codecmock/manager.go -mock_names=Manager=Manager . Manager
//

// Package codecmock is a generated GoMock package.
package codecmock

import (
	reflect "reflect"

	codec "github.com/ava-labs/avalanchego/codec"
	gomock "go.uber.org/mock/gomock"
)

// Manager is a mock of Manager interface.
type Manager struct {
	ctrl     *gomock.Controller
	recorder *ManagerMockRecorder
	isgomock struct{}
}

// ManagerMockRecorder is the mock recorder for Manager.
type ManagerMockRecorder struct {
	mock *Manager
}

// NewManager creates a new mock instance.
func NewManager(ctrl *gomock.Controller) *Manager {
	mock := &Manager{ctrl: ctrl}
	mock.recorder = &ManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Manager) EXPECT() *ManagerMockRecorder {
	return m.recorder
}

// Marshal mocks base method.
func (m *Manager) Marshal(version uint16, source any) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Marshal", version, source)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Marshal indicates an expected call of Marshal.
func (mr *ManagerMockRecorder) Marshal(version, source any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Marshal", reflect.TypeOf((*Manager)(nil).Marshal), version, source)
}

// RegisterCodec mocks base method.
func (m *Manager) RegisterCodec(version uint16, codec codec.Codec) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterCodec", version, codec)
	ret0, _ := ret[0].(error)
	return ret0
}

// RegisterCodec indicates an expected call of RegisterCodec.
func (mr *ManagerMockRecorder) RegisterCodec(version, codec any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterCodec", reflect.TypeOf((*Manager)(nil).RegisterCodec), version, codec)
}

// Size mocks base method.
func (m *Manager) Size(version uint16, value any) (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Size", version, value)
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Size indicates an expected call of Size.
func (mr *ManagerMockRecorder) Size(version, value any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Size", reflect.TypeOf((*Manager)(nil).Size), version, value)
}

// Unmarshal mocks base method.
func (m *Manager) Unmarshal(source []byte, destination any) (uint16, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Unmarshal", source, destination)
	ret0, _ := ret[0].(uint16)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Unmarshal indicates an expected call of Unmarshal.
func (mr *ManagerMockRecorder) Unmarshal(source, destination any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Unmarshal", reflect.TypeOf((*Manager)(nil).Unmarshal), source, destination)
}
