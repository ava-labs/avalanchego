// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/message (interfaces: OutboundMessage)
//
// Generated by this command:
//
//	mockgen -package=messagemock -destination=messagemock/outbound_message.go -mock_names=OutboundMessage=OutboundMessage . OutboundMessage
//

// Package messagemock is a generated GoMock package.
package messagemock

import (
	reflect "reflect"

	message "github.com/ava-labs/avalanchego/message"
	gomock "go.uber.org/mock/gomock"
)

// OutboundMessage is a mock of OutboundMessage interface.
type OutboundMessage struct {
	ctrl     *gomock.Controller
	recorder *OutboundMessageMockRecorder
}

// OutboundMessageMockRecorder is the mock recorder for OutboundMessage.
type OutboundMessageMockRecorder struct {
	mock *OutboundMessage
}

// NewOutboundMessage creates a new mock instance.
func NewOutboundMessage(ctrl *gomock.Controller) *OutboundMessage {
	mock := &OutboundMessage{ctrl: ctrl}
	mock.recorder = &OutboundMessageMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *OutboundMessage) EXPECT() *OutboundMessageMockRecorder {
	return m.recorder
}

// BypassThrottling mocks base method.
func (m *OutboundMessage) BypassThrottling() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BypassThrottling")
	ret0, _ := ret[0].(bool)
	return ret0
}

// BypassThrottling indicates an expected call of BypassThrottling.
func (mr *OutboundMessageMockRecorder) BypassThrottling() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BypassThrottling", reflect.TypeOf((*OutboundMessage)(nil).BypassThrottling))
}

// Bytes mocks base method.
func (m *OutboundMessage) Bytes() []byte {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Bytes")
	ret0, _ := ret[0].([]byte)
	return ret0
}

// Bytes indicates an expected call of Bytes.
func (mr *OutboundMessageMockRecorder) Bytes() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Bytes", reflect.TypeOf((*OutboundMessage)(nil).Bytes))
}

// BytesSavedCompression mocks base method.
func (m *OutboundMessage) BytesSavedCompression() int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BytesSavedCompression")
	ret0, _ := ret[0].(int)
	return ret0
}

// BytesSavedCompression indicates an expected call of BytesSavedCompression.
func (mr *OutboundMessageMockRecorder) BytesSavedCompression() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BytesSavedCompression", reflect.TypeOf((*OutboundMessage)(nil).BytesSavedCompression))
}

// Op mocks base method.
func (m *OutboundMessage) Op() message.Op {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Op")
	ret0, _ := ret[0].(message.Op)
	return ret0
}

// Op indicates an expected call of Op.
func (mr *OutboundMessageMockRecorder) Op() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Op", reflect.TypeOf((*OutboundMessage)(nil).Op))
}
