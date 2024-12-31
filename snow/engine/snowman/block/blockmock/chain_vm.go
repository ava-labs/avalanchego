// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ava-labs/avalanchego/snow/engine/snowman/block (interfaces: ChainVM)
//
// Generated by this command:
//
//	mockgen -package=blockmock -destination=blockmock/chain_vm.go -mock_names=ChainVM=ChainVM . ChainVM
//

// Package blockmock is a generated GoMock package.
package blockmock

import (
	context "context"
	http "net/http"
	reflect "reflect"
	time "time"

	database "github.com/ava-labs/avalanchego/database"
	ids "github.com/ava-labs/avalanchego/ids"
	snow "github.com/ava-labs/avalanchego/snow"
	snowman "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	common "github.com/ava-labs/avalanchego/snow/engine/common"
	version "github.com/ava-labs/avalanchego/version"
	gomock "go.uber.org/mock/gomock"
)

// ChainVM is a mock of ChainVM interface.
type ChainVM struct {
	ctrl     *gomock.Controller
	recorder *ChainVMMockRecorder
}

// ChainVMMockRecorder is the mock recorder for ChainVM.
type ChainVMMockRecorder struct {
	mock *ChainVM
}

// NewChainVM creates a new mock instance.
func NewChainVM(ctrl *gomock.Controller) *ChainVM {
	mock := &ChainVM{ctrl: ctrl}
	mock.recorder = &ChainVMMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *ChainVM) EXPECT() *ChainVMMockRecorder {
	return m.recorder
}

// AppGossip mocks base method.
func (m *ChainVM) AppGossip(arg0 context.Context, arg1 ids.NodeID, arg2 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AppGossip", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// AppGossip indicates an expected call of AppGossip.
func (mr *ChainVMMockRecorder) AppGossip(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AppGossip", reflect.TypeOf((*ChainVM)(nil).AppGossip), arg0, arg1, arg2)
}

// AppRequest mocks base method.
func (m *ChainVM) AppRequest(arg0 context.Context, arg1 ids.NodeID, arg2 uint32, arg3 time.Time, arg4 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AppRequest", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(error)
	return ret0
}

// AppRequest indicates an expected call of AppRequest.
func (mr *ChainVMMockRecorder) AppRequest(arg0, arg1, arg2, arg3, arg4 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AppRequest", reflect.TypeOf((*ChainVM)(nil).AppRequest), arg0, arg1, arg2, arg3, arg4)
}

// AppRequestFailed mocks base method.
func (m *ChainVM) AppRequestFailed(arg0 context.Context, arg1 ids.NodeID, arg2 uint32, arg3 *common.AppError) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AppRequestFailed", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// AppRequestFailed indicates an expected call of AppRequestFailed.
func (mr *ChainVMMockRecorder) AppRequestFailed(arg0, arg1, arg2, arg3 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AppRequestFailed", reflect.TypeOf((*ChainVM)(nil).AppRequestFailed), arg0, arg1, arg2, arg3)
}

// AppResponse mocks base method.
func (m *ChainVM) AppResponse(arg0 context.Context, arg1 ids.NodeID, arg2 uint32, arg3 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AppResponse", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// AppResponse indicates an expected call of AppResponse.
func (mr *ChainVMMockRecorder) AppResponse(arg0, arg1, arg2, arg3 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AppResponse", reflect.TypeOf((*ChainVM)(nil).AppResponse), arg0, arg1, arg2, arg3)
}

// BuildBlock mocks base method.
func (m *ChainVM) BuildBlock(arg0 context.Context) (snowman.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BuildBlock", arg0)
	ret0, _ := ret[0].(snowman.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BuildBlock indicates an expected call of BuildBlock.
func (mr *ChainVMMockRecorder) BuildBlock(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BuildBlock", reflect.TypeOf((*ChainVM)(nil).BuildBlock), arg0)
}

// Connected mocks base method.
func (m *ChainVM) Connected(arg0 context.Context, arg1 ids.NodeID, arg2 *version.Application) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Connected", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// Connected indicates an expected call of Connected.
func (mr *ChainVMMockRecorder) Connected(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Connected", reflect.TypeOf((*ChainVM)(nil).Connected), arg0, arg1, arg2)
}

// CreateHandlers mocks base method.
func (m *ChainVM) CreateHandlers(arg0 context.Context) (map[string]http.Handler, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateHandlers", arg0)
	ret0, _ := ret[0].(map[string]http.Handler)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateHandlers indicates an expected call of CreateHandlers.
func (mr *ChainVMMockRecorder) CreateHandlers(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateHandlers", reflect.TypeOf((*ChainVM)(nil).CreateHandlers), arg0)
}

// Disconnected mocks base method.
func (m *ChainVM) Disconnected(arg0 context.Context, arg1 ids.NodeID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Disconnected", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Disconnected indicates an expected call of Disconnected.
func (mr *ChainVMMockRecorder) Disconnected(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Disconnected", reflect.TypeOf((*ChainVM)(nil).Disconnected), arg0, arg1)
}

// GetBlock mocks base method.
func (m *ChainVM) GetBlock(arg0 context.Context, arg1 ids.ID) (snowman.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlock", arg0, arg1)
	ret0, _ := ret[0].(snowman.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBlock indicates an expected call of GetBlock.
func (mr *ChainVMMockRecorder) GetBlock(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlock", reflect.TypeOf((*ChainVM)(nil).GetBlock), arg0, arg1)
}

// GetBlockIDAtHeight mocks base method.
func (m *ChainVM) GetBlockIDAtHeight(arg0 context.Context, arg1 uint64) (ids.ID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlockIDAtHeight", arg0, arg1)
	ret0, _ := ret[0].(ids.ID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBlockIDAtHeight indicates an expected call of GetBlockIDAtHeight.
func (mr *ChainVMMockRecorder) GetBlockIDAtHeight(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlockIDAtHeight", reflect.TypeOf((*ChainVM)(nil).GetBlockIDAtHeight), arg0, arg1)
}

// HealthCheck mocks base method.
func (m *ChainVM) HealthCheck(arg0 context.Context) (any, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HealthCheck", arg0)
	ret0, _ := ret[0].(any)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// HealthCheck indicates an expected call of HealthCheck.
func (mr *ChainVMMockRecorder) HealthCheck(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HealthCheck", reflect.TypeOf((*ChainVM)(nil).HealthCheck), arg0)
}

// Initialize mocks base method.
func (m *ChainVM) Initialize(arg0 context.Context, arg1 *snow.Context, arg2 database.Database, arg3, arg4, arg5 []byte, arg6 chan<- common.Message, arg7 []*common.Fx, arg8 common.AppSender) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Initialize", arg0, arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8)
	ret0, _ := ret[0].(error)
	return ret0
}

// Initialize indicates an expected call of Initialize.
func (mr *ChainVMMockRecorder) Initialize(arg0, arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Initialize", reflect.TypeOf((*ChainVM)(nil).Initialize), arg0, arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8)
}

// LastAccepted mocks base method.
func (m *ChainVM) LastAccepted(arg0 context.Context) (ids.ID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LastAccepted", arg0)
	ret0, _ := ret[0].(ids.ID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LastAccepted indicates an expected call of LastAccepted.
func (mr *ChainVMMockRecorder) LastAccepted(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LastAccepted", reflect.TypeOf((*ChainVM)(nil).LastAccepted), arg0)
}

// ParseBlock mocks base method.
func (m *ChainVM) ParseBlock(arg0 context.Context, arg1 []byte) (snowman.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ParseBlock", arg0, arg1)
	ret0, _ := ret[0].(snowman.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ParseBlock indicates an expected call of ParseBlock.
func (mr *ChainVMMockRecorder) ParseBlock(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ParseBlock", reflect.TypeOf((*ChainVM)(nil).ParseBlock), arg0, arg1)
}

// SetPreference mocks base method.
func (m *ChainVM) SetPreference(arg0 context.Context, arg1 ids.ID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetPreference", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetPreference indicates an expected call of SetPreference.
func (mr *ChainVMMockRecorder) SetPreference(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetPreference", reflect.TypeOf((*ChainVM)(nil).SetPreference), arg0, arg1)
}

// SetState mocks base method.
func (m *ChainVM) SetState(arg0 context.Context, arg1 snow.State) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetState", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetState indicates an expected call of SetState.
func (mr *ChainVMMockRecorder) SetState(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetState", reflect.TypeOf((*ChainVM)(nil).SetState), arg0, arg1)
}

// Shutdown mocks base method.
func (m *ChainVM) Shutdown(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Shutdown", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Shutdown indicates an expected call of Shutdown.
func (mr *ChainVMMockRecorder) Shutdown(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Shutdown", reflect.TypeOf((*ChainVM)(nil).Shutdown), arg0)
}

// Version mocks base method.
func (m *ChainVM) Version(arg0 context.Context) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Version", arg0)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Version indicates an expected call of Version.
func (mr *ChainVMMockRecorder) Version(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Version", reflect.TypeOf((*ChainVM)(nil).Version), arg0)
}
