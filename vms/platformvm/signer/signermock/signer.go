// Code generated by MockGen. DO NOT EDIT.
// Source: vms/platformvm/signer/signer.go
//
// Generated by this command:
//
//	mockgen -source=vms/platformvm/signer/signer.go -destination=vms/platformvm/signer/signermock/signer.go -package=signermock -exclude_interfaces= -mock_names=Signer=Signer
//

// Package signermock is a generated GoMock package.
package signermock

import (
	reflect "reflect"

	bls "github.com/ava-labs/avalanchego/utils/crypto/bls"
	gomock "go.uber.org/mock/gomock"
)

// Signer is a mock of Signer interface.
type Signer struct {
	ctrl     *gomock.Controller
	recorder *SignerMockRecorder
}

// SignerMockRecorder is the mock recorder for Signer.
type SignerMockRecorder struct {
	mock *Signer
}

// NewSigner creates a new mock instance.
func NewSigner(ctrl *gomock.Controller) *Signer {
	mock := &Signer{ctrl: ctrl}
	mock.recorder = &SignerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Signer) EXPECT() *SignerMockRecorder {
	return m.recorder
}

// Key mocks base method.
func (m *Signer) Key() *bls.PublicKey {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Key")
	ret0, _ := ret[0].(*bls.PublicKey)
	return ret0
}

// Key indicates an expected call of Key.
func (mr *SignerMockRecorder) Key() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Key", reflect.TypeOf((*Signer)(nil).Key))
}

// Verify mocks base method.
func (m *Signer) Verify() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Verify")
	ret0, _ := ret[0].(error)
	return ret0
}

// Verify indicates an expected call of Verify.
func (mr *SignerMockRecorder) Verify() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*Signer)(nil).Verify))
}
