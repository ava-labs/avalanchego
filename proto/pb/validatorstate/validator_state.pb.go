// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.33.0
// 	protoc        (unknown)
// source: validatorstate/validator_state.proto

package validatorstate

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type GetMinimumHeightResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Height uint64 `protobuf:"varint,1,opt,name=height,proto3" json:"height,omitempty"`
}

func (x *GetMinimumHeightResponse) Reset() {
	*x = GetMinimumHeightResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GetMinimumHeightResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetMinimumHeightResponse) ProtoMessage() {}

func (x *GetMinimumHeightResponse) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetMinimumHeightResponse.ProtoReflect.Descriptor instead.
func (*GetMinimumHeightResponse) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{0}
}

func (x *GetMinimumHeightResponse) GetHeight() uint64 {
	if x != nil {
		return x.Height
	}
	return 0
}

type GetCurrentHeightResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Height uint64 `protobuf:"varint,1,opt,name=height,proto3" json:"height,omitempty"`
}

func (x *GetCurrentHeightResponse) Reset() {
	*x = GetCurrentHeightResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GetCurrentHeightResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetCurrentHeightResponse) ProtoMessage() {}

func (x *GetCurrentHeightResponse) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetCurrentHeightResponse.ProtoReflect.Descriptor instead.
func (*GetCurrentHeightResponse) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{1}
}

func (x *GetCurrentHeightResponse) GetHeight() uint64 {
	if x != nil {
		return x.Height
	}
	return 0
}

type GetSubnetIDRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	ChainId []byte `protobuf:"bytes,1,opt,name=chain_id,json=chainId,proto3" json:"chain_id,omitempty"`
}

func (x *GetSubnetIDRequest) Reset() {
	*x = GetSubnetIDRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GetSubnetIDRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetSubnetIDRequest) ProtoMessage() {}

func (x *GetSubnetIDRequest) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetSubnetIDRequest.ProtoReflect.Descriptor instead.
func (*GetSubnetIDRequest) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{2}
}

func (x *GetSubnetIDRequest) GetChainId() []byte {
	if x != nil {
		return x.ChainId
	}
	return nil
}

type GetSubnetIDResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	SubnetId []byte `protobuf:"bytes,1,opt,name=subnet_id,json=subnetId,proto3" json:"subnet_id,omitempty"`
}

func (x *GetSubnetIDResponse) Reset() {
	*x = GetSubnetIDResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GetSubnetIDResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetSubnetIDResponse) ProtoMessage() {}

func (x *GetSubnetIDResponse) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetSubnetIDResponse.ProtoReflect.Descriptor instead.
func (*GetSubnetIDResponse) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{3}
}

func (x *GetSubnetIDResponse) GetSubnetId() []byte {
	if x != nil {
		return x.SubnetId
	}
	return nil
}

type GetValidatorSetRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Height   uint64 `protobuf:"varint,1,opt,name=height,proto3" json:"height,omitempty"`
	SubnetId []byte `protobuf:"bytes,2,opt,name=subnet_id,json=subnetId,proto3" json:"subnet_id,omitempty"`
}

func (x *GetValidatorSetRequest) Reset() {
	*x = GetValidatorSetRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[4]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GetValidatorSetRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetValidatorSetRequest) ProtoMessage() {}

func (x *GetValidatorSetRequest) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[4]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetValidatorSetRequest.ProtoReflect.Descriptor instead.
func (*GetValidatorSetRequest) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{4}
}

func (x *GetValidatorSetRequest) GetHeight() uint64 {
	if x != nil {
		return x.Height
	}
	return 0
}

func (x *GetValidatorSetRequest) GetSubnetId() []byte {
	if x != nil {
		return x.SubnetId
	}
	return nil
}

type GetCurrentValidatorSetRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	SubnetId []byte `protobuf:"bytes,1,opt,name=subnet_id,json=subnetId,proto3" json:"subnet_id,omitempty"`
}

func (x *GetCurrentValidatorSetRequest) Reset() {
	*x = GetCurrentValidatorSetRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[5]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GetCurrentValidatorSetRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetCurrentValidatorSetRequest) ProtoMessage() {}

func (x *GetCurrentValidatorSetRequest) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[5]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetCurrentValidatorSetRequest.ProtoReflect.Descriptor instead.
func (*GetCurrentValidatorSetRequest) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{5}
}

func (x *GetCurrentValidatorSetRequest) GetSubnetId() []byte {
	if x != nil {
		return x.SubnetId
	}
	return nil
}

type Validator struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	NodeId    []byte `protobuf:"bytes,1,opt,name=node_id,json=nodeId,proto3" json:"node_id,omitempty"`
	Weight    uint64 `protobuf:"varint,2,opt,name=weight,proto3" json:"weight,omitempty"`
	PublicKey []byte `protobuf:"bytes,3,opt,name=public_key,json=publicKey,proto3" json:"public_key,omitempty"`
}

func (x *Validator) Reset() {
	*x = Validator{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[6]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Validator) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Validator) ProtoMessage() {}

func (x *Validator) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[6]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Validator.ProtoReflect.Descriptor instead.
func (*Validator) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{6}
}

func (x *Validator) GetNodeId() []byte {
	if x != nil {
		return x.NodeId
	}
	return nil
}

func (x *Validator) GetWeight() uint64 {
	if x != nil {
		return x.Weight
	}
	return 0
}

func (x *Validator) GetPublicKey() []byte {
	if x != nil {
		return x.PublicKey
	}
	return nil
}

type CurrentValidator struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	NodeId       []byte `protobuf:"bytes,1,opt,name=node_id,json=nodeId,proto3" json:"node_id,omitempty"`
	Weight       uint64 `protobuf:"varint,2,opt,name=weight,proto3" json:"weight,omitempty"`
	PublicKey    []byte `protobuf:"bytes,3,opt,name=public_key,json=publicKey,proto3" json:"public_key,omitempty"`
	StartTime    uint64 `protobuf:"varint,4,opt,name=start_time,json=startTime,proto3" json:"start_time,omitempty"`
	MinNonce     uint64 `protobuf:"varint,5,opt,name=min_nonce,json=minNonce,proto3" json:"min_nonce,omitempty"`
	IsActive     bool   `protobuf:"varint,6,opt,name=is_active,json=isActive,proto3" json:"is_active,omitempty"`
	ValidationId []byte `protobuf:"bytes,7,opt,name=validation_id,json=validationId,proto3" json:"validation_id,omitempty"`
	IsSov        bool   `protobuf:"varint,8,opt,name=is_sov,json=isSov,proto3" json:"is_sov,omitempty"`
}

func (x *CurrentValidator) Reset() {
	*x = CurrentValidator{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[7]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *CurrentValidator) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CurrentValidator) ProtoMessage() {}

func (x *CurrentValidator) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[7]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CurrentValidator.ProtoReflect.Descriptor instead.
func (*CurrentValidator) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{7}
}

func (x *CurrentValidator) GetNodeId() []byte {
	if x != nil {
		return x.NodeId
	}
	return nil
}

func (x *CurrentValidator) GetWeight() uint64 {
	if x != nil {
		return x.Weight
	}
	return 0
}

func (x *CurrentValidator) GetPublicKey() []byte {
	if x != nil {
		return x.PublicKey
	}
	return nil
}

func (x *CurrentValidator) GetStartTime() uint64 {
	if x != nil {
		return x.StartTime
	}
	return 0
}

func (x *CurrentValidator) GetMinNonce() uint64 {
	if x != nil {
		return x.MinNonce
	}
	return 0
}

func (x *CurrentValidator) GetIsActive() bool {
	if x != nil {
		return x.IsActive
	}
	return false
}

func (x *CurrentValidator) GetValidationId() []byte {
	if x != nil {
		return x.ValidationId
	}
	return nil
}

func (x *CurrentValidator) GetIsSov() bool {
	if x != nil {
		return x.IsSov
	}
	return false
}

type GetValidatorSetResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Validators []*Validator `protobuf:"bytes,1,rep,name=validators,proto3" json:"validators,omitempty"`
}

func (x *GetValidatorSetResponse) Reset() {
	*x = GetValidatorSetResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[8]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GetValidatorSetResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetValidatorSetResponse) ProtoMessage() {}

func (x *GetValidatorSetResponse) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[8]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetValidatorSetResponse.ProtoReflect.Descriptor instead.
func (*GetValidatorSetResponse) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{8}
}

func (x *GetValidatorSetResponse) GetValidators() []*Validator {
	if x != nil {
		return x.Validators
	}
	return nil
}

type GetCurrentValidatorSetResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Validators    []*CurrentValidator `protobuf:"bytes,1,rep,name=validators,proto3" json:"validators,omitempty"`
	CurrentHeight uint64              `protobuf:"varint,2,opt,name=current_height,json=currentHeight,proto3" json:"current_height,omitempty"`
}

func (x *GetCurrentValidatorSetResponse) Reset() {
	*x = GetCurrentValidatorSetResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_validatorstate_validator_state_proto_msgTypes[9]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GetCurrentValidatorSetResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetCurrentValidatorSetResponse) ProtoMessage() {}

func (x *GetCurrentValidatorSetResponse) ProtoReflect() protoreflect.Message {
	mi := &file_validatorstate_validator_state_proto_msgTypes[9]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetCurrentValidatorSetResponse.ProtoReflect.Descriptor instead.
func (*GetCurrentValidatorSetResponse) Descriptor() ([]byte, []int) {
	return file_validatorstate_validator_state_proto_rawDescGZIP(), []int{9}
}

func (x *GetCurrentValidatorSetResponse) GetValidators() []*CurrentValidator {
	if x != nil {
		return x.Validators
	}
	return nil
}

func (x *GetCurrentValidatorSetResponse) GetCurrentHeight() uint64 {
	if x != nil {
		return x.CurrentHeight
	}
	return 0
}

var File_validatorstate_validator_state_proto protoreflect.FileDescriptor

var file_validatorstate_validator_state_proto_rawDesc = []byte{
	0x0a, 0x24, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x74, 0x61, 0x74, 0x65,
	0x2f, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x5f, 0x73, 0x74, 0x61, 0x74, 0x65,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x0e, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f,
	0x72, 0x73, 0x74, 0x61, 0x74, 0x65, 0x1a, 0x1b, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x65, 0x6d, 0x70, 0x74, 0x79, 0x2e, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x22, 0x32, 0x0a, 0x18, 0x47, 0x65, 0x74, 0x4d, 0x69, 0x6e, 0x69, 0x6d, 0x75,
	0x6d, 0x48, 0x65, 0x69, 0x67, 0x68, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12,
	0x16, 0x0a, 0x06, 0x68, 0x65, 0x69, 0x67, 0x68, 0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x04, 0x52,
	0x06, 0x68, 0x65, 0x69, 0x67, 0x68, 0x74, 0x22, 0x32, 0x0a, 0x18, 0x47, 0x65, 0x74, 0x43, 0x75,
	0x72, 0x72, 0x65, 0x6e, 0x74, 0x48, 0x65, 0x69, 0x67, 0x68, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f,
	0x6e, 0x73, 0x65, 0x12, 0x16, 0x0a, 0x06, 0x68, 0x65, 0x69, 0x67, 0x68, 0x74, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x04, 0x52, 0x06, 0x68, 0x65, 0x69, 0x67, 0x68, 0x74, 0x22, 0x2f, 0x0a, 0x12, 0x47,
	0x65, 0x74, 0x53, 0x75, 0x62, 0x6e, 0x65, 0x74, 0x49, 0x44, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73,
	0x74, 0x12, 0x19, 0x0a, 0x08, 0x63, 0x68, 0x61, 0x69, 0x6e, 0x5f, 0x69, 0x64, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x0c, 0x52, 0x07, 0x63, 0x68, 0x61, 0x69, 0x6e, 0x49, 0x64, 0x22, 0x32, 0x0a, 0x13,
	0x47, 0x65, 0x74, 0x53, 0x75, 0x62, 0x6e, 0x65, 0x74, 0x49, 0x44, 0x52, 0x65, 0x73, 0x70, 0x6f,
	0x6e, 0x73, 0x65, 0x12, 0x1b, 0x0a, 0x09, 0x73, 0x75, 0x62, 0x6e, 0x65, 0x74, 0x5f, 0x69, 0x64,
	0x18, 0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x08, 0x73, 0x75, 0x62, 0x6e, 0x65, 0x74, 0x49, 0x64,
	0x22, 0x4d, 0x0a, 0x16, 0x47, 0x65, 0x74, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72,
	0x53, 0x65, 0x74, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x16, 0x0a, 0x06, 0x68, 0x65,
	0x69, 0x67, 0x68, 0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x04, 0x52, 0x06, 0x68, 0x65, 0x69, 0x67,
	0x68, 0x74, 0x12, 0x1b, 0x0a, 0x09, 0x73, 0x75, 0x62, 0x6e, 0x65, 0x74, 0x5f, 0x69, 0x64, 0x18,
	0x02, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x08, 0x73, 0x75, 0x62, 0x6e, 0x65, 0x74, 0x49, 0x64, 0x22,
	0x3c, 0x0a, 0x1d, 0x47, 0x65, 0x74, 0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x56, 0x61, 0x6c,
	0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x53, 0x65, 0x74, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74,
	0x12, 0x1b, 0x0a, 0x09, 0x73, 0x75, 0x62, 0x6e, 0x65, 0x74, 0x5f, 0x69, 0x64, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x0c, 0x52, 0x08, 0x73, 0x75, 0x62, 0x6e, 0x65, 0x74, 0x49, 0x64, 0x22, 0x5b, 0x0a,
	0x09, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x12, 0x17, 0x0a, 0x07, 0x6e, 0x6f,
	0x64, 0x65, 0x5f, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x06, 0x6e, 0x6f, 0x64,
	0x65, 0x49, 0x64, 0x12, 0x16, 0x0a, 0x06, 0x77, 0x65, 0x69, 0x67, 0x68, 0x74, 0x18, 0x02, 0x20,
	0x01, 0x28, 0x04, 0x52, 0x06, 0x77, 0x65, 0x69, 0x67, 0x68, 0x74, 0x12, 0x1d, 0x0a, 0x0a, 0x70,
	0x75, 0x62, 0x6c, 0x69, 0x63, 0x5f, 0x6b, 0x65, 0x79, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0c, 0x52,
	0x09, 0x70, 0x75, 0x62, 0x6c, 0x69, 0x63, 0x4b, 0x65, 0x79, 0x22, 0xf7, 0x01, 0x0a, 0x10, 0x43,
	0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x12,
	0x17, 0x0a, 0x07, 0x6e, 0x6f, 0x64, 0x65, 0x5f, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c,
	0x52, 0x06, 0x6e, 0x6f, 0x64, 0x65, 0x49, 0x64, 0x12, 0x16, 0x0a, 0x06, 0x77, 0x65, 0x69, 0x67,
	0x68, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x04, 0x52, 0x06, 0x77, 0x65, 0x69, 0x67, 0x68, 0x74,
	0x12, 0x1d, 0x0a, 0x0a, 0x70, 0x75, 0x62, 0x6c, 0x69, 0x63, 0x5f, 0x6b, 0x65, 0x79, 0x18, 0x03,
	0x20, 0x01, 0x28, 0x0c, 0x52, 0x09, 0x70, 0x75, 0x62, 0x6c, 0x69, 0x63, 0x4b, 0x65, 0x79, 0x12,
	0x1d, 0x0a, 0x0a, 0x73, 0x74, 0x61, 0x72, 0x74, 0x5f, 0x74, 0x69, 0x6d, 0x65, 0x18, 0x04, 0x20,
	0x01, 0x28, 0x04, 0x52, 0x09, 0x73, 0x74, 0x61, 0x72, 0x74, 0x54, 0x69, 0x6d, 0x65, 0x12, 0x1b,
	0x0a, 0x09, 0x6d, 0x69, 0x6e, 0x5f, 0x6e, 0x6f, 0x6e, 0x63, 0x65, 0x18, 0x05, 0x20, 0x01, 0x28,
	0x04, 0x52, 0x08, 0x6d, 0x69, 0x6e, 0x4e, 0x6f, 0x6e, 0x63, 0x65, 0x12, 0x1b, 0x0a, 0x09, 0x69,
	0x73, 0x5f, 0x61, 0x63, 0x74, 0x69, 0x76, 0x65, 0x18, 0x06, 0x20, 0x01, 0x28, 0x08, 0x52, 0x08,
	0x69, 0x73, 0x41, 0x63, 0x74, 0x69, 0x76, 0x65, 0x12, 0x23, 0x0a, 0x0d, 0x76, 0x61, 0x6c, 0x69,
	0x64, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x69, 0x64, 0x18, 0x07, 0x20, 0x01, 0x28, 0x0c, 0x52,
	0x0c, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x49, 0x64, 0x12, 0x15, 0x0a,
	0x06, 0x69, 0x73, 0x5f, 0x73, 0x6f, 0x76, 0x18, 0x08, 0x20, 0x01, 0x28, 0x08, 0x52, 0x05, 0x69,
	0x73, 0x53, 0x6f, 0x76, 0x22, 0x54, 0x0a, 0x17, 0x47, 0x65, 0x74, 0x56, 0x61, 0x6c, 0x69, 0x64,
	0x61, 0x74, 0x6f, 0x72, 0x53, 0x65, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12,
	0x39, 0x0a, 0x0a, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x18, 0x01, 0x20,
	0x03, 0x28, 0x0b, 0x32, 0x19, 0x2e, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73,
	0x74, 0x61, 0x74, 0x65, 0x2e, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x52, 0x0a,
	0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x22, 0x89, 0x01, 0x0a, 0x1e, 0x47,
	0x65, 0x74, 0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74,
	0x6f, 0x72, 0x53, 0x65, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x40, 0x0a,
	0x0a, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28,
	0x0b, 0x32, 0x20, 0x2e, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x74, 0x61,
	0x74, 0x65, 0x2e, 0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61,
	0x74, 0x6f, 0x72, 0x52, 0x0a, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x12,
	0x25, 0x0a, 0x0e, 0x63, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x5f, 0x68, 0x65, 0x69, 0x67, 0x68,
	0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x04, 0x52, 0x0d, 0x63, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74,
	0x48, 0x65, 0x69, 0x67, 0x68, 0x74, 0x32, 0xf1, 0x03, 0x0a, 0x0e, 0x56, 0x61, 0x6c, 0x69, 0x64,
	0x61, 0x74, 0x6f, 0x72, 0x53, 0x74, 0x61, 0x74, 0x65, 0x12, 0x54, 0x0a, 0x10, 0x47, 0x65, 0x74,
	0x4d, 0x69, 0x6e, 0x69, 0x6d, 0x75, 0x6d, 0x48, 0x65, 0x69, 0x67, 0x68, 0x74, 0x12, 0x16, 0x2e,
	0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e,
	0x45, 0x6d, 0x70, 0x74, 0x79, 0x1a, 0x28, 0x2e, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f,
	0x72, 0x73, 0x74, 0x61, 0x74, 0x65, 0x2e, 0x47, 0x65, 0x74, 0x4d, 0x69, 0x6e, 0x69, 0x6d, 0x75,
	0x6d, 0x48, 0x65, 0x69, 0x67, 0x68, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12,
	0x54, 0x0a, 0x10, 0x47, 0x65, 0x74, 0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x48, 0x65, 0x69,
	0x67, 0x68, 0x74, 0x12, 0x16, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x45, 0x6d, 0x70, 0x74, 0x79, 0x1a, 0x28, 0x2e, 0x76, 0x61,
	0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x74, 0x61, 0x74, 0x65, 0x2e, 0x47, 0x65, 0x74,
	0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x48, 0x65, 0x69, 0x67, 0x68, 0x74, 0x52, 0x65, 0x73,
	0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x56, 0x0a, 0x0b, 0x47, 0x65, 0x74, 0x53, 0x75, 0x62, 0x6e,
	0x65, 0x74, 0x49, 0x44, 0x12, 0x22, 0x2e, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72,
	0x73, 0x74, 0x61, 0x74, 0x65, 0x2e, 0x47, 0x65, 0x74, 0x53, 0x75, 0x62, 0x6e, 0x65, 0x74, 0x49,
	0x44, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x23, 0x2e, 0x76, 0x61, 0x6c, 0x69, 0x64,
	0x61, 0x74, 0x6f, 0x72, 0x73, 0x74, 0x61, 0x74, 0x65, 0x2e, 0x47, 0x65, 0x74, 0x53, 0x75, 0x62,
	0x6e, 0x65, 0x74, 0x49, 0x44, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x62, 0x0a,
	0x0f, 0x47, 0x65, 0x74, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x53, 0x65, 0x74,
	0x12, 0x26, 0x2e, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x74, 0x61, 0x74,
	0x65, 0x2e, 0x47, 0x65, 0x74, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x53, 0x65,
	0x74, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x27, 0x2e, 0x76, 0x61, 0x6c, 0x69, 0x64,
	0x61, 0x74, 0x6f, 0x72, 0x73, 0x74, 0x61, 0x74, 0x65, 0x2e, 0x47, 0x65, 0x74, 0x56, 0x61, 0x6c,
	0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x53, 0x65, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73,
	0x65, 0x12, 0x77, 0x0a, 0x16, 0x47, 0x65, 0x74, 0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x56,
	0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x53, 0x65, 0x74, 0x12, 0x2d, 0x2e, 0x76, 0x61,
	0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x74, 0x61, 0x74, 0x65, 0x2e, 0x47, 0x65, 0x74,
	0x43, 0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72,
	0x53, 0x65, 0x74, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x2e, 0x2e, 0x76, 0x61, 0x6c,
	0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x73, 0x74, 0x61, 0x74, 0x65, 0x2e, 0x47, 0x65, 0x74, 0x43,
	0x75, 0x72, 0x72, 0x65, 0x6e, 0x74, 0x56, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72, 0x53,
	0x65, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x42, 0x39, 0x5a, 0x37, 0x67, 0x69,
	0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x61, 0x76, 0x61, 0x2d, 0x6c, 0x61, 0x62,
	0x73, 0x2f, 0x61, 0x76, 0x61, 0x6c, 0x61, 0x6e, 0x63, 0x68, 0x65, 0x67, 0x6f, 0x2f, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x2f, 0x70, 0x62, 0x2f, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x6f, 0x72,
	0x73, 0x74, 0x61, 0x74, 0x65, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_validatorstate_validator_state_proto_rawDescOnce sync.Once
	file_validatorstate_validator_state_proto_rawDescData = file_validatorstate_validator_state_proto_rawDesc
)

func file_validatorstate_validator_state_proto_rawDescGZIP() []byte {
	file_validatorstate_validator_state_proto_rawDescOnce.Do(func() {
		file_validatorstate_validator_state_proto_rawDescData = protoimpl.X.CompressGZIP(file_validatorstate_validator_state_proto_rawDescData)
	})
	return file_validatorstate_validator_state_proto_rawDescData
}

var file_validatorstate_validator_state_proto_msgTypes = make([]protoimpl.MessageInfo, 10)
var file_validatorstate_validator_state_proto_goTypes = []interface{}{
	(*GetMinimumHeightResponse)(nil),       // 0: validatorstate.GetMinimumHeightResponse
	(*GetCurrentHeightResponse)(nil),       // 1: validatorstate.GetCurrentHeightResponse
	(*GetSubnetIDRequest)(nil),             // 2: validatorstate.GetSubnetIDRequest
	(*GetSubnetIDResponse)(nil),            // 3: validatorstate.GetSubnetIDResponse
	(*GetValidatorSetRequest)(nil),         // 4: validatorstate.GetValidatorSetRequest
	(*GetCurrentValidatorSetRequest)(nil),  // 5: validatorstate.GetCurrentValidatorSetRequest
	(*Validator)(nil),                      // 6: validatorstate.Validator
	(*CurrentValidator)(nil),               // 7: validatorstate.CurrentValidator
	(*GetValidatorSetResponse)(nil),        // 8: validatorstate.GetValidatorSetResponse
	(*GetCurrentValidatorSetResponse)(nil), // 9: validatorstate.GetCurrentValidatorSetResponse
	(*emptypb.Empty)(nil),                  // 10: google.protobuf.Empty
}
var file_validatorstate_validator_state_proto_depIdxs = []int32{
	6,  // 0: validatorstate.GetValidatorSetResponse.validators:type_name -> validatorstate.Validator
	7,  // 1: validatorstate.GetCurrentValidatorSetResponse.validators:type_name -> validatorstate.CurrentValidator
	10, // 2: validatorstate.ValidatorState.GetMinimumHeight:input_type -> google.protobuf.Empty
	10, // 3: validatorstate.ValidatorState.GetCurrentHeight:input_type -> google.protobuf.Empty
	2,  // 4: validatorstate.ValidatorState.GetSubnetID:input_type -> validatorstate.GetSubnetIDRequest
	4,  // 5: validatorstate.ValidatorState.GetValidatorSet:input_type -> validatorstate.GetValidatorSetRequest
	5,  // 6: validatorstate.ValidatorState.GetCurrentValidatorSet:input_type -> validatorstate.GetCurrentValidatorSetRequest
	0,  // 7: validatorstate.ValidatorState.GetMinimumHeight:output_type -> validatorstate.GetMinimumHeightResponse
	1,  // 8: validatorstate.ValidatorState.GetCurrentHeight:output_type -> validatorstate.GetCurrentHeightResponse
	3,  // 9: validatorstate.ValidatorState.GetSubnetID:output_type -> validatorstate.GetSubnetIDResponse
	8,  // 10: validatorstate.ValidatorState.GetValidatorSet:output_type -> validatorstate.GetValidatorSetResponse
	9,  // 11: validatorstate.ValidatorState.GetCurrentValidatorSet:output_type -> validatorstate.GetCurrentValidatorSetResponse
	7,  // [7:12] is the sub-list for method output_type
	2,  // [2:7] is the sub-list for method input_type
	2,  // [2:2] is the sub-list for extension type_name
	2,  // [2:2] is the sub-list for extension extendee
	0,  // [0:2] is the sub-list for field type_name
}

func init() { file_validatorstate_validator_state_proto_init() }
func file_validatorstate_validator_state_proto_init() {
	if File_validatorstate_validator_state_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_validatorstate_validator_state_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GetMinimumHeightResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_validatorstate_validator_state_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GetCurrentHeightResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_validatorstate_validator_state_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GetSubnetIDRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_validatorstate_validator_state_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GetSubnetIDResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_validatorstate_validator_state_proto_msgTypes[4].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GetValidatorSetRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_validatorstate_validator_state_proto_msgTypes[5].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GetCurrentValidatorSetRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_validatorstate_validator_state_proto_msgTypes[6].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Validator); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_validatorstate_validator_state_proto_msgTypes[7].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*CurrentValidator); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_validatorstate_validator_state_proto_msgTypes[8].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GetValidatorSetResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_validatorstate_validator_state_proto_msgTypes[9].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GetCurrentValidatorSetResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_validatorstate_validator_state_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   10,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_validatorstate_validator_state_proto_goTypes,
		DependencyIndexes: file_validatorstate_validator_state_proto_depIdxs,
		MessageInfos:      file_validatorstate_validator_state_proto_msgTypes,
	}.Build()
	File_validatorstate_validator_state_proto = out.File
	file_validatorstate_validator_state_proto_rawDesc = nil
	file_validatorstate_validator_state_proto_goTypes = nil
	file_validatorstate_validator_state_proto_depIdxs = nil
}
