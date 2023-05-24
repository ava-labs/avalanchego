// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.28.1
// 	protoc        (unknown)
// source: sync/sync.proto

package sync

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// Request represents a request for information during syncing.
type Request struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Types that are assignable to Message:
	//
	//	*Request_RangeProofRequest
	//	*Request_ChangeProofRequest
	Message isRequest_Message `protobuf_oneof:"message"`
}

func (x *Request) Reset() {
	*x = Request{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Request) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Request) ProtoMessage() {}

func (x *Request) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Request.ProtoReflect.Descriptor instead.
func (*Request) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{0}
}

func (m *Request) GetMessage() isRequest_Message {
	if m != nil {
		return m.Message
	}
	return nil
}

func (x *Request) GetRangeProofRequest() *RangeProofRequest {
	if x, ok := x.GetMessage().(*Request_RangeProofRequest); ok {
		return x.RangeProofRequest
	}
	return nil
}

func (x *Request) GetChangeProofRequest() *ChangeProofRequest {
	if x, ok := x.GetMessage().(*Request_ChangeProofRequest); ok {
		return x.ChangeProofRequest
	}
	return nil
}

type isRequest_Message interface {
	isRequest_Message()
}

type Request_RangeProofRequest struct {
	RangeProofRequest *RangeProofRequest `protobuf:"bytes,1,opt,name=range_proof_request,json=rangeProofRequest,proto3,oneof"`
}

type Request_ChangeProofRequest struct {
	ChangeProofRequest *ChangeProofRequest `protobuf:"bytes,2,opt,name=change_proof_request,json=changeProofRequest,proto3,oneof"`
}

func (*Request_RangeProofRequest) isRequest_Message() {}

func (*Request_ChangeProofRequest) isRequest_Message() {}

// A RangeProofRequest requests the key-value pairs in a given key range
// at a specific revision.
type RangeProofRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	RootHash   []byte `protobuf:"bytes,1,opt,name=root_hash,json=rootHash,proto3" json:"root_hash,omitempty"`
	StartKey   []byte `protobuf:"bytes,2,opt,name=start_key,json=startKey,proto3" json:"start_key,omitempty"`
	EndKey     []byte `protobuf:"bytes,3,opt,name=end_key,json=endKey,proto3" json:"end_key,omitempty"`
	KeyLimit   uint32 `protobuf:"varint,4,opt,name=key_limit,json=keyLimit,proto3" json:"key_limit,omitempty"`
	BytesLimit uint32 `protobuf:"varint,5,opt,name=bytes_limit,json=bytesLimit,proto3" json:"bytes_limit,omitempty"`
}

func (x *RangeProofRequest) Reset() {
	*x = RangeProofRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *RangeProofRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*RangeProofRequest) ProtoMessage() {}

func (x *RangeProofRequest) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use RangeProofRequest.ProtoReflect.Descriptor instead.
func (*RangeProofRequest) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{1}
}

func (x *RangeProofRequest) GetRootHash() []byte {
	if x != nil {
		return x.RootHash
	}
	return nil
}

func (x *RangeProofRequest) GetStartKey() []byte {
	if x != nil {
		return x.StartKey
	}
	return nil
}

func (x *RangeProofRequest) GetEndKey() []byte {
	if x != nil {
		return x.EndKey
	}
	return nil
}

func (x *RangeProofRequest) GetKeyLimit() uint32 {
	if x != nil {
		return x.KeyLimit
	}
	return 0
}

func (x *RangeProofRequest) GetBytesLimit() uint32 {
	if x != nil {
		return x.BytesLimit
	}
	return 0
}

// A ChangeProofRequest requests the changes between two revisions.
type ChangeProofRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	StartRootHash []byte `protobuf:"bytes,1,opt,name=start_root_hash,json=startRootHash,proto3" json:"start_root_hash,omitempty"`
	EndRootHash   []byte `protobuf:"bytes,2,opt,name=end_root_hash,json=endRootHash,proto3" json:"end_root_hash,omitempty"`
	StartKey      []byte `protobuf:"bytes,3,opt,name=start_key,json=startKey,proto3" json:"start_key,omitempty"`
	EndKey        []byte `protobuf:"bytes,4,opt,name=end_key,json=endKey,proto3" json:"end_key,omitempty"`
	KeyLimit      uint32 `protobuf:"varint,5,opt,name=key_limit,json=keyLimit,proto3" json:"key_limit,omitempty"`
	BytesLimit    uint32 `protobuf:"varint,6,opt,name=bytes_limit,json=bytesLimit,proto3" json:"bytes_limit,omitempty"`
}

func (x *ChangeProofRequest) Reset() {
	*x = ChangeProofRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ChangeProofRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ChangeProofRequest) ProtoMessage() {}

func (x *ChangeProofRequest) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ChangeProofRequest.ProtoReflect.Descriptor instead.
func (*ChangeProofRequest) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{2}
}

func (x *ChangeProofRequest) GetStartRootHash() []byte {
	if x != nil {
		return x.StartRootHash
	}
	return nil
}

func (x *ChangeProofRequest) GetEndRootHash() []byte {
	if x != nil {
		return x.EndRootHash
	}
	return nil
}

func (x *ChangeProofRequest) GetStartKey() []byte {
	if x != nil {
		return x.StartKey
	}
	return nil
}

func (x *ChangeProofRequest) GetEndKey() []byte {
	if x != nil {
		return x.EndKey
	}
	return nil
}

func (x *ChangeProofRequest) GetKeyLimit() uint32 {
	if x != nil {
		return x.KeyLimit
	}
	return 0
}

func (x *ChangeProofRequest) GetBytesLimit() uint32 {
	if x != nil {
		return x.BytesLimit
	}
	return 0
}

// KeyValue represents a single key and its value.
type KeyValue struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Key   []byte `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	Value []byte `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

func (x *KeyValue) Reset() {
	*x = KeyValue{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *KeyValue) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*KeyValue) ProtoMessage() {}

func (x *KeyValue) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use KeyValue.ProtoReflect.Descriptor instead.
func (*KeyValue) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{3}
}

func (x *KeyValue) GetKey() []byte {
	if x != nil {
		return x.Key
	}
	return nil
}

func (x *KeyValue) GetValue() []byte {
	if x != nil {
		return x.Value
	}
	return nil
}

// KeyChange is a change for a key from one revision to another.
// If the value is None, the key was deleted.
type KeyChange struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Key   []byte      `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	Value *MaybeBytes `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

func (x *KeyChange) Reset() {
	*x = KeyChange{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[4]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *KeyChange) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*KeyChange) ProtoMessage() {}

func (x *KeyChange) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[4]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use KeyChange.ProtoReflect.Descriptor instead.
func (*KeyChange) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{4}
}

func (x *KeyChange) GetKey() []byte {
	if x != nil {
		return x.Key
	}
	return nil
}

func (x *KeyChange) GetValue() *MaybeBytes {
	if x != nil {
		return x.Value
	}
	return nil
}

// SerializedPath is the serialized representation of a path.
type SerializedPath struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	NibbleLength uint32 `protobuf:"varint,1,opt,name=nibble_length,json=nibbleLength,proto3" json:"nibble_length,omitempty"`
	Value        []byte `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

func (x *SerializedPath) Reset() {
	*x = SerializedPath{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[5]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SerializedPath) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SerializedPath) ProtoMessage() {}

func (x *SerializedPath) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[5]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SerializedPath.ProtoReflect.Descriptor instead.
func (*SerializedPath) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{5}
}

func (x *SerializedPath) GetNibbleLength() uint32 {
	if x != nil {
		return x.NibbleLength
	}
	return 0
}

func (x *SerializedPath) GetValue() []byte {
	if x != nil {
		return x.Value
	}
	return nil
}

// MaybeBytes is an option wrapping bytes.
type MaybeBytes struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Value []byte `protobuf:"bytes,1,opt,name=value,proto3" json:"value,omitempty"`
	// If false, this is None.
	IsNothing bool `protobuf:"varint,2,opt,name=is_nothing,json=isNothing,proto3" json:"is_nothing,omitempty"`
}

func (x *MaybeBytes) Reset() {
	*x = MaybeBytes{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[6]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MaybeBytes) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MaybeBytes) ProtoMessage() {}

func (x *MaybeBytes) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[6]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MaybeBytes.ProtoReflect.Descriptor instead.
func (*MaybeBytes) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{6}
}

func (x *MaybeBytes) GetValue() []byte {
	if x != nil {
		return x.Value
	}
	return nil
}

func (x *MaybeBytes) GetIsNothing() bool {
	if x != nil {
		return x.IsNothing
	}
	return false
}

// ProofNode is a node in a merkle proof.
type ProofNode struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Key         *SerializedPath   `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	ValueOrHash *MaybeBytes       `protobuf:"bytes,2,opt,name=value_or_hash,json=valueOrHash,proto3" json:"value_or_hash,omitempty"`
	Children    map[uint32][]byte `protobuf:"bytes,3,rep,name=children,proto3" json:"children,omitempty" protobuf_key:"varint,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

func (x *ProofNode) Reset() {
	*x = ProofNode{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[7]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ProofNode) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ProofNode) ProtoMessage() {}

func (x *ProofNode) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[7]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ProofNode.ProtoReflect.Descriptor instead.
func (*ProofNode) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{7}
}

func (x *ProofNode) GetKey() *SerializedPath {
	if x != nil {
		return x.Key
	}
	return nil
}

func (x *ProofNode) GetValueOrHash() *MaybeBytes {
	if x != nil {
		return x.ValueOrHash
	}
	return nil
}

func (x *ProofNode) GetChildren() map[uint32][]byte {
	if x != nil {
		return x.Children
	}
	return nil
}

// RangeProof is the response to a RangeProofRequest.
type RangeProof struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Start     []*ProofNode `protobuf:"bytes,1,rep,name=start,proto3" json:"start,omitempty"`
	End       []*ProofNode `protobuf:"bytes,2,rep,name=end,proto3" json:"end,omitempty"`
	KeyValues []*KeyValue  `protobuf:"bytes,3,rep,name=key_values,json=keyValues,proto3" json:"key_values,omitempty"`
}

func (x *RangeProof) Reset() {
	*x = RangeProof{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[8]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *RangeProof) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*RangeProof) ProtoMessage() {}

func (x *RangeProof) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[8]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use RangeProof.ProtoReflect.Descriptor instead.
func (*RangeProof) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{8}
}

func (x *RangeProof) GetStart() []*ProofNode {
	if x != nil {
		return x.Start
	}
	return nil
}

func (x *RangeProof) GetEnd() []*ProofNode {
	if x != nil {
		return x.End
	}
	return nil
}

func (x *RangeProof) GetKeyValues() []*KeyValue {
	if x != nil {
		return x.KeyValues
	}
	return nil
}

// ChangeProof is a possible response to a ChangeProofRequest.
// It only consists of a proof of the smallest changed key,
// the highest changed key, and the keys that have changed
// between those. Some keys may be deleted (hence
// the use of KeyChange instead of KeyValue).
type ChangeProof struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	HadRootsInHistory bool         `protobuf:"varint,1,opt,name=had_roots_in_history,json=hadRootsInHistory,proto3" json:"had_roots_in_history,omitempty"` // TODO remove
	StartProof        []*ProofNode `protobuf:"bytes,2,rep,name=start_proof,json=startProof,proto3" json:"start_proof,omitempty"`
	EndProof          []*ProofNode `protobuf:"bytes,3,rep,name=end_proof,json=endProof,proto3" json:"end_proof,omitempty"`
	KeyChanges        []*KeyChange `protobuf:"bytes,4,rep,name=key_changes,json=keyChanges,proto3" json:"key_changes,omitempty"`
}

func (x *ChangeProof) Reset() {
	*x = ChangeProof{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[9]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ChangeProof) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ChangeProof) ProtoMessage() {}

func (x *ChangeProof) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[9]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ChangeProof.ProtoReflect.Descriptor instead.
func (*ChangeProof) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{9}
}

func (x *ChangeProof) GetHadRootsInHistory() bool {
	if x != nil {
		return x.HadRootsInHistory
	}
	return false
}

func (x *ChangeProof) GetStartProof() []*ProofNode {
	if x != nil {
		return x.StartProof
	}
	return nil
}

func (x *ChangeProof) GetEndProof() []*ProofNode {
	if x != nil {
		return x.EndProof
	}
	return nil
}

func (x *ChangeProof) GetKeyChanges() []*KeyChange {
	if x != nil {
		return x.KeyChanges
	}
	return nil
}

// ChangeProofResponse is the response for a ChangeProofRequest.
type ChangeProofResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Types that are assignable to Response:
	//
	//	*ChangeProofResponse_ChangeProof
	//	*ChangeProofResponse_RangeProof
	Response isChangeProofResponse_Response `protobuf_oneof:"response"`
}

func (x *ChangeProofResponse) Reset() {
	*x = ChangeProofResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_sync_sync_proto_msgTypes[10]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ChangeProofResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ChangeProofResponse) ProtoMessage() {}

func (x *ChangeProofResponse) ProtoReflect() protoreflect.Message {
	mi := &file_sync_sync_proto_msgTypes[10]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ChangeProofResponse.ProtoReflect.Descriptor instead.
func (*ChangeProofResponse) Descriptor() ([]byte, []int) {
	return file_sync_sync_proto_rawDescGZIP(), []int{10}
}

func (m *ChangeProofResponse) GetResponse() isChangeProofResponse_Response {
	if m != nil {
		return m.Response
	}
	return nil
}

func (x *ChangeProofResponse) GetChangeProof() *ChangeProof {
	if x, ok := x.GetResponse().(*ChangeProofResponse_ChangeProof); ok {
		return x.ChangeProof
	}
	return nil
}

func (x *ChangeProofResponse) GetRangeProof() *RangeProof {
	if x, ok := x.GetResponse().(*ChangeProofResponse_RangeProof); ok {
		return x.RangeProof
	}
	return nil
}

type isChangeProofResponse_Response interface {
	isChangeProofResponse_Response()
}

type ChangeProofResponse_ChangeProof struct {
	ChangeProof *ChangeProof `protobuf:"bytes,1,opt,name=change_proof,json=changeProof,proto3,oneof"`
}

type ChangeProofResponse_RangeProof struct {
	RangeProof *RangeProof `protobuf:"bytes,2,opt,name=range_proof,json=rangeProof,proto3,oneof"`
}

func (*ChangeProofResponse_ChangeProof) isChangeProofResponse_Response() {}

func (*ChangeProofResponse_RangeProof) isChangeProofResponse_Response() {}

var File_sync_sync_proto protoreflect.FileDescriptor

var file_sync_sync_proto_rawDesc = []byte{
	0x0a, 0x0f, 0x73, 0x79, 0x6e, 0x63, 0x2f, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x12, 0x04, 0x73, 0x79, 0x6e, 0x63, 0x22, 0xad, 0x01, 0x0a, 0x07, 0x52, 0x65, 0x71, 0x75,
	0x65, 0x73, 0x74, 0x12, 0x49, 0x0a, 0x13, 0x72, 0x61, 0x6e, 0x67, 0x65, 0x5f, 0x70, 0x72, 0x6f,
	0x6f, 0x66, 0x5f, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b,
	0x32, 0x17, 0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x52, 0x61, 0x6e, 0x67, 0x65, 0x50, 0x72, 0x6f,
	0x6f, 0x66, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x48, 0x00, 0x52, 0x11, 0x72, 0x61, 0x6e,
	0x67, 0x65, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x4c,
	0x0a, 0x14, 0x63, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x5f, 0x70, 0x72, 0x6f, 0x6f, 0x66, 0x5f, 0x72,
	0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x18, 0x2e, 0x73,
	0x79, 0x6e, 0x63, 0x2e, 0x43, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x52,
	0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x48, 0x00, 0x52, 0x12, 0x63, 0x68, 0x61, 0x6e, 0x67, 0x65,
	0x50, 0x72, 0x6f, 0x6f, 0x66, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x42, 0x09, 0x0a, 0x07,
	0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x22, 0xa4, 0x01, 0x0a, 0x11, 0x52, 0x61, 0x6e, 0x67,
	0x65, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x1b, 0x0a,
	0x09, 0x72, 0x6f, 0x6f, 0x74, 0x5f, 0x68, 0x61, 0x73, 0x68, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c,
	0x52, 0x08, 0x72, 0x6f, 0x6f, 0x74, 0x48, 0x61, 0x73, 0x68, 0x12, 0x1b, 0x0a, 0x09, 0x73, 0x74,
	0x61, 0x72, 0x74, 0x5f, 0x6b, 0x65, 0x79, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x08, 0x73,
	0x74, 0x61, 0x72, 0x74, 0x4b, 0x65, 0x79, 0x12, 0x17, 0x0a, 0x07, 0x65, 0x6e, 0x64, 0x5f, 0x6b,
	0x65, 0x79, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x06, 0x65, 0x6e, 0x64, 0x4b, 0x65, 0x79,
	0x12, 0x1b, 0x0a, 0x09, 0x6b, 0x65, 0x79, 0x5f, 0x6c, 0x69, 0x6d, 0x69, 0x74, 0x18, 0x04, 0x20,
	0x01, 0x28, 0x0d, 0x52, 0x08, 0x6b, 0x65, 0x79, 0x4c, 0x69, 0x6d, 0x69, 0x74, 0x12, 0x1f, 0x0a,
	0x0b, 0x62, 0x79, 0x74, 0x65, 0x73, 0x5f, 0x6c, 0x69, 0x6d, 0x69, 0x74, 0x18, 0x05, 0x20, 0x01,
	0x28, 0x0d, 0x52, 0x0a, 0x62, 0x79, 0x74, 0x65, 0x73, 0x4c, 0x69, 0x6d, 0x69, 0x74, 0x22, 0xd4,
	0x01, 0x0a, 0x12, 0x43, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x52, 0x65,
	0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x26, 0x0a, 0x0f, 0x73, 0x74, 0x61, 0x72, 0x74, 0x5f, 0x72,
	0x6f, 0x6f, 0x74, 0x5f, 0x68, 0x61, 0x73, 0x68, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x0d,
	0x73, 0x74, 0x61, 0x72, 0x74, 0x52, 0x6f, 0x6f, 0x74, 0x48, 0x61, 0x73, 0x68, 0x12, 0x22, 0x0a,
	0x0d, 0x65, 0x6e, 0x64, 0x5f, 0x72, 0x6f, 0x6f, 0x74, 0x5f, 0x68, 0x61, 0x73, 0x68, 0x18, 0x02,
	0x20, 0x01, 0x28, 0x0c, 0x52, 0x0b, 0x65, 0x6e, 0x64, 0x52, 0x6f, 0x6f, 0x74, 0x48, 0x61, 0x73,
	0x68, 0x12, 0x1b, 0x0a, 0x09, 0x73, 0x74, 0x61, 0x72, 0x74, 0x5f, 0x6b, 0x65, 0x79, 0x18, 0x03,
	0x20, 0x01, 0x28, 0x0c, 0x52, 0x08, 0x73, 0x74, 0x61, 0x72, 0x74, 0x4b, 0x65, 0x79, 0x12, 0x17,
	0x0a, 0x07, 0x65, 0x6e, 0x64, 0x5f, 0x6b, 0x65, 0x79, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0c, 0x52,
	0x06, 0x65, 0x6e, 0x64, 0x4b, 0x65, 0x79, 0x12, 0x1b, 0x0a, 0x09, 0x6b, 0x65, 0x79, 0x5f, 0x6c,
	0x69, 0x6d, 0x69, 0x74, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0d, 0x52, 0x08, 0x6b, 0x65, 0x79, 0x4c,
	0x69, 0x6d, 0x69, 0x74, 0x12, 0x1f, 0x0a, 0x0b, 0x62, 0x79, 0x74, 0x65, 0x73, 0x5f, 0x6c, 0x69,
	0x6d, 0x69, 0x74, 0x18, 0x06, 0x20, 0x01, 0x28, 0x0d, 0x52, 0x0a, 0x62, 0x79, 0x74, 0x65, 0x73,
	0x4c, 0x69, 0x6d, 0x69, 0x74, 0x22, 0x32, 0x0a, 0x08, 0x4b, 0x65, 0x79, 0x56, 0x61, 0x6c, 0x75,
	0x65, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x03,
	0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01,
	0x28, 0x0c, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x22, 0x45, 0x0a, 0x09, 0x4b, 0x65, 0x79,
	0x43, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x0c, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x26, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75,
	0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x10, 0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x4d,
	0x61, 0x79, 0x62, 0x65, 0x42, 0x79, 0x74, 0x65, 0x73, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65,
	0x22, 0x4b, 0x0a, 0x0e, 0x53, 0x65, 0x72, 0x69, 0x61, 0x6c, 0x69, 0x7a, 0x65, 0x64, 0x50, 0x61,
	0x74, 0x68, 0x12, 0x23, 0x0a, 0x0d, 0x6e, 0x69, 0x62, 0x62, 0x6c, 0x65, 0x5f, 0x6c, 0x65, 0x6e,
	0x67, 0x74, 0x68, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0d, 0x52, 0x0c, 0x6e, 0x69, 0x62, 0x62, 0x6c,
	0x65, 0x4c, 0x65, 0x6e, 0x67, 0x74, 0x68, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65,
	0x18, 0x02, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x22, 0x41, 0x0a,
	0x0a, 0x4d, 0x61, 0x79, 0x62, 0x65, 0x42, 0x79, 0x74, 0x65, 0x73, 0x12, 0x14, 0x0a, 0x05, 0x76,
	0x61, 0x6c, 0x75, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75,
	0x65, 0x12, 0x1d, 0x0a, 0x0a, 0x69, 0x73, 0x5f, 0x6e, 0x6f, 0x74, 0x68, 0x69, 0x6e, 0x67, 0x18,
	0x02, 0x20, 0x01, 0x28, 0x08, 0x52, 0x09, 0x69, 0x73, 0x4e, 0x6f, 0x74, 0x68, 0x69, 0x6e, 0x67,
	0x22, 0xe1, 0x01, 0x0a, 0x09, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x4e, 0x6f, 0x64, 0x65, 0x12, 0x26,
	0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x73, 0x79,
	0x6e, 0x63, 0x2e, 0x53, 0x65, 0x72, 0x69, 0x61, 0x6c, 0x69, 0x7a, 0x65, 0x64, 0x50, 0x61, 0x74,
	0x68, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x34, 0x0a, 0x0d, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x5f,
	0x6f, 0x72, 0x5f, 0x68, 0x61, 0x73, 0x68, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x10, 0x2e,
	0x73, 0x79, 0x6e, 0x63, 0x2e, 0x4d, 0x61, 0x79, 0x62, 0x65, 0x42, 0x79, 0x74, 0x65, 0x73, 0x52,
	0x0b, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x4f, 0x72, 0x48, 0x61, 0x73, 0x68, 0x12, 0x39, 0x0a, 0x08,
	0x63, 0x68, 0x69, 0x6c, 0x64, 0x72, 0x65, 0x6e, 0x18, 0x03, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x1d,
	0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x4e, 0x6f, 0x64, 0x65, 0x2e,
	0x43, 0x68, 0x69, 0x6c, 0x64, 0x72, 0x65, 0x6e, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x08, 0x63,
	0x68, 0x69, 0x6c, 0x64, 0x72, 0x65, 0x6e, 0x1a, 0x3b, 0x0a, 0x0d, 0x43, 0x68, 0x69, 0x6c, 0x64,
	0x72, 0x65, 0x6e, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x0d, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61,
	0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65,
	0x3a, 0x02, 0x38, 0x01, 0x22, 0x85, 0x01, 0x0a, 0x0a, 0x52, 0x61, 0x6e, 0x67, 0x65, 0x50, 0x72,
	0x6f, 0x6f, 0x66, 0x12, 0x25, 0x0a, 0x05, 0x73, 0x74, 0x61, 0x72, 0x74, 0x18, 0x01, 0x20, 0x03,
	0x28, 0x0b, 0x32, 0x0f, 0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x4e,
	0x6f, 0x64, 0x65, 0x52, 0x05, 0x73, 0x74, 0x61, 0x72, 0x74, 0x12, 0x21, 0x0a, 0x03, 0x65, 0x6e,
	0x64, 0x18, 0x02, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x0f, 0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x50,
	0x72, 0x6f, 0x6f, 0x66, 0x4e, 0x6f, 0x64, 0x65, 0x52, 0x03, 0x65, 0x6e, 0x64, 0x12, 0x2d, 0x0a,
	0x0a, 0x6b, 0x65, 0x79, 0x5f, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x73, 0x18, 0x03, 0x20, 0x03, 0x28,
	0x0b, 0x32, 0x0e, 0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x4b, 0x65, 0x79, 0x56, 0x61, 0x6c, 0x75,
	0x65, 0x52, 0x09, 0x6b, 0x65, 0x79, 0x56, 0x61, 0x6c, 0x75, 0x65, 0x73, 0x22, 0xd0, 0x01, 0x0a,
	0x0b, 0x43, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x12, 0x2f, 0x0a, 0x14,
	0x68, 0x61, 0x64, 0x5f, 0x72, 0x6f, 0x6f, 0x74, 0x73, 0x5f, 0x69, 0x6e, 0x5f, 0x68, 0x69, 0x73,
	0x74, 0x6f, 0x72, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x08, 0x52, 0x11, 0x68, 0x61, 0x64, 0x52,
	0x6f, 0x6f, 0x74, 0x73, 0x49, 0x6e, 0x48, 0x69, 0x73, 0x74, 0x6f, 0x72, 0x79, 0x12, 0x30, 0x0a,
	0x0b, 0x73, 0x74, 0x61, 0x72, 0x74, 0x5f, 0x70, 0x72, 0x6f, 0x6f, 0x66, 0x18, 0x02, 0x20, 0x03,
	0x28, 0x0b, 0x32, 0x0f, 0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x4e,
	0x6f, 0x64, 0x65, 0x52, 0x0a, 0x73, 0x74, 0x61, 0x72, 0x74, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x12,
	0x2c, 0x0a, 0x09, 0x65, 0x6e, 0x64, 0x5f, 0x70, 0x72, 0x6f, 0x6f, 0x66, 0x18, 0x03, 0x20, 0x03,
	0x28, 0x0b, 0x32, 0x0f, 0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x4e,
	0x6f, 0x64, 0x65, 0x52, 0x08, 0x65, 0x6e, 0x64, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x12, 0x30, 0x0a,
	0x0b, 0x6b, 0x65, 0x79, 0x5f, 0x63, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x73, 0x18, 0x04, 0x20, 0x03,
	0x28, 0x0b, 0x32, 0x0f, 0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x4b, 0x65, 0x79, 0x43, 0x68, 0x61,
	0x6e, 0x67, 0x65, 0x52, 0x0a, 0x6b, 0x65, 0x79, 0x43, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x73, 0x22,
	0x8e, 0x01, 0x0a, 0x13, 0x43, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x52,
	0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x36, 0x0a, 0x0c, 0x63, 0x68, 0x61, 0x6e, 0x67,
	0x65, 0x5f, 0x70, 0x72, 0x6f, 0x6f, 0x66, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x11, 0x2e,
	0x73, 0x79, 0x6e, 0x63, 0x2e, 0x43, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x50, 0x72, 0x6f, 0x6f, 0x66,
	0x48, 0x00, 0x52, 0x0b, 0x63, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x12,
	0x33, 0x0a, 0x0b, 0x72, 0x61, 0x6e, 0x67, 0x65, 0x5f, 0x70, 0x72, 0x6f, 0x6f, 0x66, 0x18, 0x02,
	0x20, 0x01, 0x28, 0x0b, 0x32, 0x10, 0x2e, 0x73, 0x79, 0x6e, 0x63, 0x2e, 0x52, 0x61, 0x6e, 0x67,
	0x65, 0x50, 0x72, 0x6f, 0x6f, 0x66, 0x48, 0x00, 0x52, 0x0a, 0x72, 0x61, 0x6e, 0x67, 0x65, 0x50,
	0x72, 0x6f, 0x6f, 0x66, 0x42, 0x0a, 0x0a, 0x08, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65,
	0x42, 0x2f, 0x5a, 0x2d, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x61,
	0x76, 0x61, 0x2d, 0x6c, 0x61, 0x62, 0x73, 0x2f, 0x61, 0x76, 0x61, 0x6c, 0x61, 0x6e, 0x63, 0x68,
	0x65, 0x67, 0x6f, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x70, 0x62, 0x2f, 0x73, 0x79, 0x6e,
	0x63, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_sync_sync_proto_rawDescOnce sync.Once
	file_sync_sync_proto_rawDescData = file_sync_sync_proto_rawDesc
)

func file_sync_sync_proto_rawDescGZIP() []byte {
	file_sync_sync_proto_rawDescOnce.Do(func() {
		file_sync_sync_proto_rawDescData = protoimpl.X.CompressGZIP(file_sync_sync_proto_rawDescData)
	})
	return file_sync_sync_proto_rawDescData
}

var file_sync_sync_proto_msgTypes = make([]protoimpl.MessageInfo, 12)
var file_sync_sync_proto_goTypes = []interface{}{
	(*Request)(nil),             // 0: sync.Request
	(*RangeProofRequest)(nil),   // 1: sync.RangeProofRequest
	(*ChangeProofRequest)(nil),  // 2: sync.ChangeProofRequest
	(*KeyValue)(nil),            // 3: sync.KeyValue
	(*KeyChange)(nil),           // 4: sync.KeyChange
	(*SerializedPath)(nil),      // 5: sync.SerializedPath
	(*MaybeBytes)(nil),          // 6: sync.MaybeBytes
	(*ProofNode)(nil),           // 7: sync.ProofNode
	(*RangeProof)(nil),          // 8: sync.RangeProof
	(*ChangeProof)(nil),         // 9: sync.ChangeProof
	(*ChangeProofResponse)(nil), // 10: sync.ChangeProofResponse
	nil,                         // 11: sync.ProofNode.ChildrenEntry
}
var file_sync_sync_proto_depIdxs = []int32{
	1,  // 0: sync.Request.range_proof_request:type_name -> sync.RangeProofRequest
	2,  // 1: sync.Request.change_proof_request:type_name -> sync.ChangeProofRequest
	6,  // 2: sync.KeyChange.value:type_name -> sync.MaybeBytes
	5,  // 3: sync.ProofNode.key:type_name -> sync.SerializedPath
	6,  // 4: sync.ProofNode.value_or_hash:type_name -> sync.MaybeBytes
	11, // 5: sync.ProofNode.children:type_name -> sync.ProofNode.ChildrenEntry
	7,  // 6: sync.RangeProof.start:type_name -> sync.ProofNode
	7,  // 7: sync.RangeProof.end:type_name -> sync.ProofNode
	3,  // 8: sync.RangeProof.key_values:type_name -> sync.KeyValue
	7,  // 9: sync.ChangeProof.start_proof:type_name -> sync.ProofNode
	7,  // 10: sync.ChangeProof.end_proof:type_name -> sync.ProofNode
	4,  // 11: sync.ChangeProof.key_changes:type_name -> sync.KeyChange
	9,  // 12: sync.ChangeProofResponse.change_proof:type_name -> sync.ChangeProof
	8,  // 13: sync.ChangeProofResponse.range_proof:type_name -> sync.RangeProof
	14, // [14:14] is the sub-list for method output_type
	14, // [14:14] is the sub-list for method input_type
	14, // [14:14] is the sub-list for extension type_name
	14, // [14:14] is the sub-list for extension extendee
	0,  // [0:14] is the sub-list for field type_name
}

func init() { file_sync_sync_proto_init() }
func file_sync_sync_proto_init() {
	if File_sync_sync_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_sync_sync_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Request); i {
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
		file_sync_sync_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*RangeProofRequest); i {
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
		file_sync_sync_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ChangeProofRequest); i {
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
		file_sync_sync_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*KeyValue); i {
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
		file_sync_sync_proto_msgTypes[4].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*KeyChange); i {
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
		file_sync_sync_proto_msgTypes[5].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SerializedPath); i {
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
		file_sync_sync_proto_msgTypes[6].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MaybeBytes); i {
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
		file_sync_sync_proto_msgTypes[7].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ProofNode); i {
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
		file_sync_sync_proto_msgTypes[8].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*RangeProof); i {
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
		file_sync_sync_proto_msgTypes[9].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ChangeProof); i {
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
		file_sync_sync_proto_msgTypes[10].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ChangeProofResponse); i {
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
	file_sync_sync_proto_msgTypes[0].OneofWrappers = []interface{}{
		(*Request_RangeProofRequest)(nil),
		(*Request_ChangeProofRequest)(nil),
	}
	file_sync_sync_proto_msgTypes[10].OneofWrappers = []interface{}{
		(*ChangeProofResponse_ChangeProof)(nil),
		(*ChangeProofResponse_RangeProof)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_sync_sync_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   12,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_sync_sync_proto_goTypes,
		DependencyIndexes: file_sync_sync_proto_depIdxs,
		MessageInfos:      file_sync_sync_proto_msgTypes,
	}.Build()
	File_sync_sync_proto = out.File
	file_sync_sync_proto_rawDesc = nil
	file_sync_sync_proto_goTypes = nil
	file_sync_sync_proto_depIdxs = nil
}
