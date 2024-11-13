// Code generated by MockGen. DO NOT EDIT.
// Source: vms/platformvm/state/state.go
//
// Generated by this command:
//
//	mockgen -source=vms/platformvm/state/state.go -destination=vms/platformvm/state/mock_chain.go -package=state -exclude_interfaces=State -mock_names=MockChain=MockChain
//

// Package state is a generated GoMock package.
package state

import (
	reflect "reflect"
	time "time"

	ids "github.com/ava-labs/avalanchego/ids"
	iterator "github.com/ava-labs/avalanchego/utils/iterator"
	avax "github.com/ava-labs/avalanchego/vms/components/avax"
	gas "github.com/ava-labs/avalanchego/vms/components/gas"
	fx "github.com/ava-labs/avalanchego/vms/platformvm/fx"
	status "github.com/ava-labs/avalanchego/vms/platformvm/status"
	txs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
	gomock "go.uber.org/mock/gomock"
)

// MockChain is a mock of Chain interface.
type MockChain struct {
	ctrl     *gomock.Controller
	recorder *MockChainMockRecorder
}

// MockChainMockRecorder is the mock recorder for MockChain.
type MockChainMockRecorder struct {
	mock *MockChain
}

// NewMockChain creates a new mock instance.
func NewMockChain(ctrl *gomock.Controller) *MockChain {
	mock := &MockChain{ctrl: ctrl}
	mock.recorder = &MockChainMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockChain) EXPECT() *MockChainMockRecorder {
	return m.recorder
}

// AddChain mocks base method.
func (m *MockChain) AddChain(createChainTx *txs.Tx) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddChain", createChainTx)
}

// AddChain indicates an expected call of AddChain.
func (mr *MockChainMockRecorder) AddChain(createChainTx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddChain", reflect.TypeOf((*MockChain)(nil).AddChain), createChainTx)
}

// AddRewardUTXO mocks base method.
func (m *MockChain) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddRewardUTXO", txID, utxo)
}

// AddRewardUTXO indicates an expected call of AddRewardUTXO.
func (mr *MockChainMockRecorder) AddRewardUTXO(txID, utxo any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddRewardUTXO", reflect.TypeOf((*MockChain)(nil).AddRewardUTXO), txID, utxo)
}

// AddSubnet mocks base method.
func (m *MockChain) AddSubnet(subnetID ids.ID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddSubnet", subnetID)
}

// AddSubnet indicates an expected call of AddSubnet.
func (mr *MockChainMockRecorder) AddSubnet(subnetID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddSubnet", reflect.TypeOf((*MockChain)(nil).AddSubnet), subnetID)
}

// AddSubnetTransformation mocks base method.
func (m *MockChain) AddSubnetTransformation(transformSubnetTx *txs.Tx) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddSubnetTransformation", transformSubnetTx)
}

// AddSubnetTransformation indicates an expected call of AddSubnetTransformation.
func (mr *MockChainMockRecorder) AddSubnetTransformation(transformSubnetTx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddSubnetTransformation", reflect.TypeOf((*MockChain)(nil).AddSubnetTransformation), transformSubnetTx)
}

// AddTx mocks base method.
func (m *MockChain) AddTx(tx *txs.Tx, status status.Status) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddTx", tx, status)
}

// AddTx indicates an expected call of AddTx.
func (mr *MockChainMockRecorder) AddTx(tx, status any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddTx", reflect.TypeOf((*MockChain)(nil).AddTx), tx, status)
}

// AddUTXO mocks base method.
func (m *MockChain) AddUTXO(utxo *avax.UTXO) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddUTXO", utxo)
}

// AddUTXO indicates an expected call of AddUTXO.
func (mr *MockChainMockRecorder) AddUTXO(utxo any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddUTXO", reflect.TypeOf((*MockChain)(nil).AddUTXO), utxo)
}

// DeleteCurrentDelegator mocks base method.
func (m *MockChain) DeleteCurrentDelegator(staker *Staker) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "DeleteCurrentDelegator", staker)
}

// DeleteCurrentDelegator indicates an expected call of DeleteCurrentDelegator.
func (mr *MockChainMockRecorder) DeleteCurrentDelegator(staker any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteCurrentDelegator", reflect.TypeOf((*MockChain)(nil).DeleteCurrentDelegator), staker)
}

// DeleteCurrentValidator mocks base method.
func (m *MockChain) DeleteCurrentValidator(staker *Staker) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "DeleteCurrentValidator", staker)
}

// DeleteCurrentValidator indicates an expected call of DeleteCurrentValidator.
func (mr *MockChainMockRecorder) DeleteCurrentValidator(staker any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteCurrentValidator", reflect.TypeOf((*MockChain)(nil).DeleteCurrentValidator), staker)
}

// DeleteExpiry mocks base method.
func (m *MockChain) DeleteExpiry(arg0 ExpiryEntry) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "DeleteExpiry", arg0)
}

// DeleteExpiry indicates an expected call of DeleteExpiry.
func (mr *MockChainMockRecorder) DeleteExpiry(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteExpiry", reflect.TypeOf((*MockChain)(nil).DeleteExpiry), arg0)
}

// DeletePendingDelegator mocks base method.
func (m *MockChain) DeletePendingDelegator(staker *Staker) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "DeletePendingDelegator", staker)
}

// DeletePendingDelegator indicates an expected call of DeletePendingDelegator.
func (mr *MockChainMockRecorder) DeletePendingDelegator(staker any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeletePendingDelegator", reflect.TypeOf((*MockChain)(nil).DeletePendingDelegator), staker)
}

// DeletePendingValidator mocks base method.
func (m *MockChain) DeletePendingValidator(staker *Staker) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "DeletePendingValidator", staker)
}

// DeletePendingValidator indicates an expected call of DeletePendingValidator.
func (mr *MockChainMockRecorder) DeletePendingValidator(staker any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeletePendingValidator", reflect.TypeOf((*MockChain)(nil).DeletePendingValidator), staker)
}

// DeleteUTXO mocks base method.
func (m *MockChain) DeleteUTXO(utxoID ids.ID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "DeleteUTXO", utxoID)
}

// DeleteUTXO indicates an expected call of DeleteUTXO.
func (mr *MockChainMockRecorder) DeleteUTXO(utxoID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteUTXO", reflect.TypeOf((*MockChain)(nil).DeleteUTXO), utxoID)
}

// GetAccruedFees mocks base method.
func (m *MockChain) GetAccruedFees() uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAccruedFees")
	ret0, _ := ret[0].(uint64)
	return ret0
}

// GetAccruedFees indicates an expected call of GetAccruedFees.
func (mr *MockChainMockRecorder) GetAccruedFees() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAccruedFees", reflect.TypeOf((*MockChain)(nil).GetAccruedFees))
}

// GetActiveSubnetOnlyValidatorsIterator mocks base method.
func (m *MockChain) GetActiveSubnetOnlyValidatorsIterator() (iterator.Iterator[SubnetOnlyValidator], error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetActiveSubnetOnlyValidatorsIterator")
	ret0, _ := ret[0].(iterator.Iterator[SubnetOnlyValidator])
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetActiveSubnetOnlyValidatorsIterator indicates an expected call of GetActiveSubnetOnlyValidatorsIterator.
func (mr *MockChainMockRecorder) GetActiveSubnetOnlyValidatorsIterator() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetActiveSubnetOnlyValidatorsIterator", reflect.TypeOf((*MockChain)(nil).GetActiveSubnetOnlyValidatorsIterator))
}

// GetCurrentDelegatorIterator mocks base method.
func (m *MockChain) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCurrentDelegatorIterator", subnetID, nodeID)
	ret0, _ := ret[0].(iterator.Iterator[*Staker])
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetCurrentDelegatorIterator indicates an expected call of GetCurrentDelegatorIterator.
func (mr *MockChainMockRecorder) GetCurrentDelegatorIterator(subnetID, nodeID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCurrentDelegatorIterator", reflect.TypeOf((*MockChain)(nil).GetCurrentDelegatorIterator), subnetID, nodeID)
}

// GetCurrentStakerIterator mocks base method.
func (m *MockChain) GetCurrentStakerIterator() (iterator.Iterator[*Staker], error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCurrentStakerIterator")
	ret0, _ := ret[0].(iterator.Iterator[*Staker])
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetCurrentStakerIterator indicates an expected call of GetCurrentStakerIterator.
func (mr *MockChainMockRecorder) GetCurrentStakerIterator() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCurrentStakerIterator", reflect.TypeOf((*MockChain)(nil).GetCurrentStakerIterator))
}

// GetCurrentSupply mocks base method.
func (m *MockChain) GetCurrentSupply(subnetID ids.ID) (uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCurrentSupply", subnetID)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetCurrentSupply indicates an expected call of GetCurrentSupply.
func (mr *MockChainMockRecorder) GetCurrentSupply(subnetID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCurrentSupply", reflect.TypeOf((*MockChain)(nil).GetCurrentSupply), subnetID)
}

// GetCurrentValidator mocks base method.
func (m *MockChain) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCurrentValidator", subnetID, nodeID)
	ret0, _ := ret[0].(*Staker)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetCurrentValidator indicates an expected call of GetCurrentValidator.
func (mr *MockChainMockRecorder) GetCurrentValidator(subnetID, nodeID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCurrentValidator", reflect.TypeOf((*MockChain)(nil).GetCurrentValidator), subnetID, nodeID)
}

// GetDelegateeReward mocks base method.
func (m *MockChain) GetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID) (uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetDelegateeReward", subnetID, nodeID)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetDelegateeReward indicates an expected call of GetDelegateeReward.
func (mr *MockChainMockRecorder) GetDelegateeReward(subnetID, nodeID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetDelegateeReward", reflect.TypeOf((*MockChain)(nil).GetDelegateeReward), subnetID, nodeID)
}

// GetExpiryIterator mocks base method.
func (m *MockChain) GetExpiryIterator() (iterator.Iterator[ExpiryEntry], error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetExpiryIterator")
	ret0, _ := ret[0].(iterator.Iterator[ExpiryEntry])
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetExpiryIterator indicates an expected call of GetExpiryIterator.
func (mr *MockChainMockRecorder) GetExpiryIterator() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetExpiryIterator", reflect.TypeOf((*MockChain)(nil).GetExpiryIterator))
}

// GetFeeState mocks base method.
func (m *MockChain) GetFeeState() gas.State {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetFeeState")
	ret0, _ := ret[0].(gas.State)
	return ret0
}

// GetFeeState indicates an expected call of GetFeeState.
func (mr *MockChainMockRecorder) GetFeeState() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetFeeState", reflect.TypeOf((*MockChain)(nil).GetFeeState))
}

// GetPendingDelegatorIterator mocks base method.
func (m *MockChain) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPendingDelegatorIterator", subnetID, nodeID)
	ret0, _ := ret[0].(iterator.Iterator[*Staker])
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetPendingDelegatorIterator indicates an expected call of GetPendingDelegatorIterator.
func (mr *MockChainMockRecorder) GetPendingDelegatorIterator(subnetID, nodeID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPendingDelegatorIterator", reflect.TypeOf((*MockChain)(nil).GetPendingDelegatorIterator), subnetID, nodeID)
}

// GetPendingStakerIterator mocks base method.
func (m *MockChain) GetPendingStakerIterator() (iterator.Iterator[*Staker], error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPendingStakerIterator")
	ret0, _ := ret[0].(iterator.Iterator[*Staker])
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetPendingStakerIterator indicates an expected call of GetPendingStakerIterator.
func (mr *MockChainMockRecorder) GetPendingStakerIterator() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPendingStakerIterator", reflect.TypeOf((*MockChain)(nil).GetPendingStakerIterator))
}

// GetPendingValidator mocks base method.
func (m *MockChain) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPendingValidator", subnetID, nodeID)
	ret0, _ := ret[0].(*Staker)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetPendingValidator indicates an expected call of GetPendingValidator.
func (mr *MockChainMockRecorder) GetPendingValidator(subnetID, nodeID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPendingValidator", reflect.TypeOf((*MockChain)(nil).GetPendingValidator), subnetID, nodeID)
}

// GetSoVExcess mocks base method.
func (m *MockChain) GetSoVExcess() gas.Gas {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSoVExcess")
	ret0, _ := ret[0].(gas.Gas)
	return ret0
}

// GetSoVExcess indicates an expected call of GetSoVExcess.
func (mr *MockChainMockRecorder) GetSoVExcess() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSoVExcess", reflect.TypeOf((*MockChain)(nil).GetSoVExcess))
}

// GetSubnetToL1Conversion mocks base method.
func (m *MockChain) GetSubnetToL1Conversion(subnetID ids.ID) (SubnetToL1Conversion, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSubnetToL1Conversion", subnetID)
	ret0, _ := ret[0].(SubnetToL1Conversion)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetSubnetToL1Conversion indicates an expected call of GetSubnetToL1Conversion.
func (mr *MockChainMockRecorder) GetSubnetToL1Conversion(subnetID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSubnetToL1Conversion", reflect.TypeOf((*MockChain)(nil).GetSubnetToL1Conversion), subnetID)
}

// GetSubnetOnlyValidator mocks base method.
func (m *MockChain) GetSubnetOnlyValidator(validationID ids.ID) (SubnetOnlyValidator, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSubnetOnlyValidator", validationID)
	ret0, _ := ret[0].(SubnetOnlyValidator)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetSubnetOnlyValidator indicates an expected call of GetSubnetOnlyValidator.
func (mr *MockChainMockRecorder) GetSubnetOnlyValidator(validationID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSubnetOnlyValidator", reflect.TypeOf((*MockChain)(nil).GetSubnetOnlyValidator), validationID)
}

// GetSubnetOwner mocks base method.
func (m *MockChain) GetSubnetOwner(subnetID ids.ID) (fx.Owner, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSubnetOwner", subnetID)
	ret0, _ := ret[0].(fx.Owner)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetSubnetOwner indicates an expected call of GetSubnetOwner.
func (mr *MockChainMockRecorder) GetSubnetOwner(subnetID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSubnetOwner", reflect.TypeOf((*MockChain)(nil).GetSubnetOwner), subnetID)
}

// GetSubnetTransformation mocks base method.
func (m *MockChain) GetSubnetTransformation(subnetID ids.ID) (*txs.Tx, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSubnetTransformation", subnetID)
	ret0, _ := ret[0].(*txs.Tx)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetSubnetTransformation indicates an expected call of GetSubnetTransformation.
func (mr *MockChainMockRecorder) GetSubnetTransformation(subnetID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSubnetTransformation", reflect.TypeOf((*MockChain)(nil).GetSubnetTransformation), subnetID)
}

// GetTimestamp mocks base method.
func (m *MockChain) GetTimestamp() time.Time {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetTimestamp")
	ret0, _ := ret[0].(time.Time)
	return ret0
}

// GetTimestamp indicates an expected call of GetTimestamp.
func (mr *MockChainMockRecorder) GetTimestamp() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetTimestamp", reflect.TypeOf((*MockChain)(nil).GetTimestamp))
}

// GetTx mocks base method.
func (m *MockChain) GetTx(txID ids.ID) (*txs.Tx, status.Status, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetTx", txID)
	ret0, _ := ret[0].(*txs.Tx)
	ret1, _ := ret[1].(status.Status)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetTx indicates an expected call of GetTx.
func (mr *MockChainMockRecorder) GetTx(txID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetTx", reflect.TypeOf((*MockChain)(nil).GetTx), txID)
}

// GetUTXO mocks base method.
func (m *MockChain) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetUTXO", utxoID)
	ret0, _ := ret[0].(*avax.UTXO)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetUTXO indicates an expected call of GetUTXO.
func (mr *MockChainMockRecorder) GetUTXO(utxoID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetUTXO", reflect.TypeOf((*MockChain)(nil).GetUTXO), utxoID)
}

// HasExpiry mocks base method.
func (m *MockChain) HasExpiry(arg0 ExpiryEntry) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HasExpiry", arg0)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// HasExpiry indicates an expected call of HasExpiry.
func (mr *MockChainMockRecorder) HasExpiry(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasExpiry", reflect.TypeOf((*MockChain)(nil).HasExpiry), arg0)
}

// HasSubnetOnlyValidator mocks base method.
func (m *MockChain) HasSubnetOnlyValidator(subnetID ids.ID, nodeID ids.NodeID) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HasSubnetOnlyValidator", subnetID, nodeID)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// HasSubnetOnlyValidator indicates an expected call of HasSubnetOnlyValidator.
func (mr *MockChainMockRecorder) HasSubnetOnlyValidator(subnetID, nodeID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasSubnetOnlyValidator", reflect.TypeOf((*MockChain)(nil).HasSubnetOnlyValidator), subnetID, nodeID)
}

// NumActiveSubnetOnlyValidators mocks base method.
func (m *MockChain) NumActiveSubnetOnlyValidators() int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NumActiveSubnetOnlyValidators")
	ret0, _ := ret[0].(int)
	return ret0
}

// NumActiveSubnetOnlyValidators indicates an expected call of NumActiveSubnetOnlyValidators.
func (mr *MockChainMockRecorder) NumActiveSubnetOnlyValidators() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NumActiveSubnetOnlyValidators", reflect.TypeOf((*MockChain)(nil).NumActiveSubnetOnlyValidators))
}

// PutCurrentDelegator mocks base method.
func (m *MockChain) PutCurrentDelegator(staker *Staker) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "PutCurrentDelegator", staker)
}

// PutCurrentDelegator indicates an expected call of PutCurrentDelegator.
func (mr *MockChainMockRecorder) PutCurrentDelegator(staker any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PutCurrentDelegator", reflect.TypeOf((*MockChain)(nil).PutCurrentDelegator), staker)
}

// PutCurrentValidator mocks base method.
func (m *MockChain) PutCurrentValidator(staker *Staker) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PutCurrentValidator", staker)
	ret0, _ := ret[0].(error)
	return ret0
}

// PutCurrentValidator indicates an expected call of PutCurrentValidator.
func (mr *MockChainMockRecorder) PutCurrentValidator(staker any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PutCurrentValidator", reflect.TypeOf((*MockChain)(nil).PutCurrentValidator), staker)
}

// PutExpiry mocks base method.
func (m *MockChain) PutExpiry(arg0 ExpiryEntry) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "PutExpiry", arg0)
}

// PutExpiry indicates an expected call of PutExpiry.
func (mr *MockChainMockRecorder) PutExpiry(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PutExpiry", reflect.TypeOf((*MockChain)(nil).PutExpiry), arg0)
}

// PutPendingDelegator mocks base method.
func (m *MockChain) PutPendingDelegator(staker *Staker) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "PutPendingDelegator", staker)
}

// PutPendingDelegator indicates an expected call of PutPendingDelegator.
func (mr *MockChainMockRecorder) PutPendingDelegator(staker any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PutPendingDelegator", reflect.TypeOf((*MockChain)(nil).PutPendingDelegator), staker)
}

// PutPendingValidator mocks base method.
func (m *MockChain) PutPendingValidator(staker *Staker) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PutPendingValidator", staker)
	ret0, _ := ret[0].(error)
	return ret0
}

// PutPendingValidator indicates an expected call of PutPendingValidator.
func (mr *MockChainMockRecorder) PutPendingValidator(staker any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PutPendingValidator", reflect.TypeOf((*MockChain)(nil).PutPendingValidator), staker)
}

// PutSubnetOnlyValidator mocks base method.
func (m *MockChain) PutSubnetOnlyValidator(sov SubnetOnlyValidator) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PutSubnetOnlyValidator", sov)
	ret0, _ := ret[0].(error)
	return ret0
}

// PutSubnetOnlyValidator indicates an expected call of PutSubnetOnlyValidator.
func (mr *MockChainMockRecorder) PutSubnetOnlyValidator(sov any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PutSubnetOnlyValidator", reflect.TypeOf((*MockChain)(nil).PutSubnetOnlyValidator), sov)
}

// SetAccruedFees mocks base method.
func (m *MockChain) SetAccruedFees(f uint64) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetAccruedFees", f)
}

// SetAccruedFees indicates an expected call of SetAccruedFees.
func (mr *MockChainMockRecorder) SetAccruedFees(f any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetAccruedFees", reflect.TypeOf((*MockChain)(nil).SetAccruedFees), f)
}

// SetCurrentSupply mocks base method.
func (m *MockChain) SetCurrentSupply(subnetID ids.ID, cs uint64) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetCurrentSupply", subnetID, cs)
}

// SetCurrentSupply indicates an expected call of SetCurrentSupply.
func (mr *MockChainMockRecorder) SetCurrentSupply(subnetID, cs any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetCurrentSupply", reflect.TypeOf((*MockChain)(nil).SetCurrentSupply), subnetID, cs)
}

// SetDelegateeReward mocks base method.
func (m *MockChain) SetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID, amount uint64) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetDelegateeReward", subnetID, nodeID, amount)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetDelegateeReward indicates an expected call of SetDelegateeReward.
func (mr *MockChainMockRecorder) SetDelegateeReward(subnetID, nodeID, amount any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetDelegateeReward", reflect.TypeOf((*MockChain)(nil).SetDelegateeReward), subnetID, nodeID, amount)
}

// SetFeeState mocks base method.
func (m *MockChain) SetFeeState(f gas.State) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetFeeState", f)
}

// SetFeeState indicates an expected call of SetFeeState.
func (mr *MockChainMockRecorder) SetFeeState(f any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetFeeState", reflect.TypeOf((*MockChain)(nil).SetFeeState), f)
}

// SetSoVExcess mocks base method.
func (m *MockChain) SetSoVExcess(e gas.Gas) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetSoVExcess", e)
}

// SetSoVExcess indicates an expected call of SetSoVExcess.
func (mr *MockChainMockRecorder) SetSoVExcess(e any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetSoVExcess", reflect.TypeOf((*MockChain)(nil).SetSoVExcess), e)
}

// SetSubnetToL1Conversion mocks base method.
func (m *MockChain) SetSubnetToL1Conversion(subnetID ids.ID, c SubnetToL1Conversion) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetSubnetToL1Conversion", subnetID, c)
}

// SetSubnetToL1Conversion indicates an expected call of SetSubnetToL1Conversion.
func (mr *MockChainMockRecorder) SetSubnetToL1Conversion(subnetID, c any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetSubnetToL1Conversion", reflect.TypeOf((*MockChain)(nil).SetSubnetToL1Conversion), subnetID, c)
}

// SetSubnetOwner mocks base method.
func (m *MockChain) SetSubnetOwner(subnetID ids.ID, owner fx.Owner) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetSubnetOwner", subnetID, owner)
}

// SetSubnetOwner indicates an expected call of SetSubnetOwner.
func (mr *MockChainMockRecorder) SetSubnetOwner(subnetID, owner any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetSubnetOwner", reflect.TypeOf((*MockChain)(nil).SetSubnetOwner), subnetID, owner)
}

// SetTimestamp mocks base method.
func (m *MockChain) SetTimestamp(tm time.Time) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetTimestamp", tm)
}

// SetTimestamp indicates an expected call of SetTimestamp.
func (mr *MockChainMockRecorder) SetTimestamp(tm any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetTimestamp", reflect.TypeOf((*MockChain)(nil).SetTimestamp), tm)
}

// WeightOfSubnetOnlyValidators mocks base method.
func (m *MockChain) WeightOfSubnetOnlyValidators(subnetID ids.ID) (uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WeightOfSubnetOnlyValidators", subnetID)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// WeightOfSubnetOnlyValidators indicates an expected call of WeightOfSubnetOnlyValidators.
func (mr *MockChainMockRecorder) WeightOfSubnetOnlyValidators(subnetID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WeightOfSubnetOnlyValidators", reflect.TypeOf((*MockChain)(nil).WeightOfSubnetOnlyValidators), subnetID)
}
