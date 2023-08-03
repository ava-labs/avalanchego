// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/network/p2p (interfaces: Handler)

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"
	time "time"

	ids "github.com/ava-labs/avalanchego/ids"
	gomock "github.com/golang/mock/gomock"
)

// MockHandler is a mock of Handler interface.
type MockHandler struct {
	ctrl     *gomock.Controller
	recorder *MockHandlerMockRecorder
}

// MockHandlerMockRecorder is the mock recorder for MockHandler.
type MockHandlerMockRecorder struct {
	mock *MockHandler
}

// NewMockHandler creates a new mock instance.
func NewMockHandler(ctrl *gomock.Controller) *MockHandler {
	mock := &MockHandler{ctrl: ctrl}
	mock.recorder = &MockHandlerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockHandler) EXPECT() *MockHandlerMockRecorder {
	return m.recorder
}

// AppGossip mocks base method.
func (m *MockHandler) AppGossip(arg0 context.Context, arg1 ids.NodeID, arg2 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AppGossip", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// AppGossip indicates an expected call of AppGossip.
func (mr *MockHandlerMockRecorder) AppGossip(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AppGossip", reflect.TypeOf((*MockHandler)(nil).AppGossip), arg0, arg1, arg2)
}

// AppRequest mocks base method.
func (m *MockHandler) AppRequest(arg0 context.Context, arg1 ids.NodeID, arg2 time.Time, arg3 []byte) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AppRequest", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AppRequest indicates an expected call of AppRequest.
func (mr *MockHandlerMockRecorder) AppRequest(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AppRequest", reflect.TypeOf((*MockHandler)(nil).AppRequest), arg0, arg1, arg2, arg3)
}

// CrossChainAppRequest mocks base method.
func (m *MockHandler) CrossChainAppRequest(arg0 context.Context, arg1 ids.ID, arg2 time.Time, arg3 []byte) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CrossChainAppRequest", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CrossChainAppRequest indicates an expected call of CrossChainAppRequest.
func (mr *MockHandlerMockRecorder) CrossChainAppRequest(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CrossChainAppRequest", reflect.TypeOf((*MockHandler)(nil).CrossChainAppRequest), arg0, arg1, arg2, arg3)
}
