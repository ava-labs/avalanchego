// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.33.0
// 	protoc        (unknown)
// source: xsvm/service.proto

package xsvm

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

type PingRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Message string `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
}

func (x *PingRequest) Reset() {
	*x = PingRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_xsvm_service_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *PingRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PingRequest) ProtoMessage() {}

func (x *PingRequest) ProtoReflect() protoreflect.Message {
	mi := &file_xsvm_service_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PingRequest.ProtoReflect.Descriptor instead.
func (*PingRequest) Descriptor() ([]byte, []int) {
	return file_xsvm_service_proto_rawDescGZIP(), []int{0}
}

func (x *PingRequest) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

type PingReply struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Message string `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
}

func (x *PingReply) Reset() {
	*x = PingReply{}
	if protoimpl.UnsafeEnabled {
		mi := &file_xsvm_service_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *PingReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PingReply) ProtoMessage() {}

func (x *PingReply) ProtoReflect() protoreflect.Message {
	mi := &file_xsvm_service_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PingReply.ProtoReflect.Descriptor instead.
func (*PingReply) Descriptor() ([]byte, []int) {
	return file_xsvm_service_proto_rawDescGZIP(), []int{1}
}

func (x *PingReply) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

type StreamPingRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Message string `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
}

func (x *StreamPingRequest) Reset() {
	*x = StreamPingRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_xsvm_service_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *StreamPingRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StreamPingRequest) ProtoMessage() {}

func (x *StreamPingRequest) ProtoReflect() protoreflect.Message {
	mi := &file_xsvm_service_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StreamPingRequest.ProtoReflect.Descriptor instead.
func (*StreamPingRequest) Descriptor() ([]byte, []int) {
	return file_xsvm_service_proto_rawDescGZIP(), []int{2}
}

func (x *StreamPingRequest) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

type StreamPingReply struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Message string `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
}

func (x *StreamPingReply) Reset() {
	*x = StreamPingReply{}
	if protoimpl.UnsafeEnabled {
		mi := &file_xsvm_service_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *StreamPingReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StreamPingReply) ProtoMessage() {}

func (x *StreamPingReply) ProtoReflect() protoreflect.Message {
	mi := &file_xsvm_service_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StreamPingReply.ProtoReflect.Descriptor instead.
func (*StreamPingReply) Descriptor() ([]byte, []int) {
	return file_xsvm_service_proto_rawDescGZIP(), []int{3}
}

func (x *StreamPingReply) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

var File_xsvm_service_proto protoreflect.FileDescriptor

var file_xsvm_service_proto_rawDesc = []byte{
	0x0a, 0x12, 0x78, 0x73, 0x76, 0x6d, 0x2f, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x2e, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x12, 0x04, 0x78, 0x73, 0x76, 0x6d, 0x22, 0x27, 0x0a, 0x0b, 0x50, 0x69,
	0x6e, 0x67, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x18, 0x0a, 0x07, 0x6d, 0x65, 0x73,
	0x73, 0x61, 0x67, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x6d, 0x65, 0x73, 0x73,
	0x61, 0x67, 0x65, 0x22, 0x25, 0x0a, 0x09, 0x50, 0x69, 0x6e, 0x67, 0x52, 0x65, 0x70, 0x6c, 0x79,
	0x12, 0x18, 0x0a, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x22, 0x2d, 0x0a, 0x11, 0x53, 0x74,
	0x72, 0x65, 0x61, 0x6d, 0x50, 0x69, 0x6e, 0x67, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12,
	0x18, 0x0a, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x22, 0x2b, 0x0a, 0x0f, 0x53, 0x74, 0x72,
	0x65, 0x61, 0x6d, 0x50, 0x69, 0x6e, 0x67, 0x52, 0x65, 0x70, 0x6c, 0x79, 0x12, 0x18, 0x0a, 0x07,
	0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x6d,
	0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x32, 0x74, 0x0a, 0x04, 0x50, 0x69, 0x6e, 0x67, 0x12, 0x2a,
	0x0a, 0x04, 0x50, 0x69, 0x6e, 0x67, 0x12, 0x11, 0x2e, 0x78, 0x73, 0x76, 0x6d, 0x2e, 0x50, 0x69,
	0x6e, 0x67, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x0f, 0x2e, 0x78, 0x73, 0x76, 0x6d,
	0x2e, 0x50, 0x69, 0x6e, 0x67, 0x52, 0x65, 0x70, 0x6c, 0x79, 0x12, 0x40, 0x0a, 0x0a, 0x53, 0x74,
	0x72, 0x65, 0x61, 0x6d, 0x50, 0x69, 0x6e, 0x67, 0x12, 0x17, 0x2e, 0x78, 0x73, 0x76, 0x6d, 0x2e,
	0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x50, 0x69, 0x6e, 0x67, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73,
	0x74, 0x1a, 0x15, 0x2e, 0x78, 0x73, 0x76, 0x6d, 0x2e, 0x53, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x50,
	0x69, 0x6e, 0x67, 0x52, 0x65, 0x70, 0x6c, 0x79, 0x28, 0x01, 0x30, 0x01, 0x42, 0x2f, 0x5a, 0x2d,
	0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x61, 0x76, 0x61, 0x2d, 0x6c,
	0x61, 0x62, 0x73, 0x2f, 0x61, 0x76, 0x61, 0x6c, 0x61, 0x6e, 0x63, 0x68, 0x65, 0x67, 0x6f, 0x2f,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x70, 0x62, 0x2f, 0x78, 0x73, 0x76, 0x6d, 0x62, 0x06, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_xsvm_service_proto_rawDescOnce sync.Once
	file_xsvm_service_proto_rawDescData = file_xsvm_service_proto_rawDesc
)

func file_xsvm_service_proto_rawDescGZIP() []byte {
	file_xsvm_service_proto_rawDescOnce.Do(func() {
		file_xsvm_service_proto_rawDescData = protoimpl.X.CompressGZIP(file_xsvm_service_proto_rawDescData)
	})
	return file_xsvm_service_proto_rawDescData
}

var file_xsvm_service_proto_msgTypes = make([]protoimpl.MessageInfo, 4)
var file_xsvm_service_proto_goTypes = []interface{}{
	(*PingRequest)(nil),       // 0: xsvm.PingRequest
	(*PingReply)(nil),         // 1: xsvm.PingReply
	(*StreamPingRequest)(nil), // 2: xsvm.StreamPingRequest
	(*StreamPingReply)(nil),   // 3: xsvm.StreamPingReply
}
var file_xsvm_service_proto_depIdxs = []int32{
	0, // 0: xsvm.Ping.Ping:input_type -> xsvm.PingRequest
	2, // 1: xsvm.Ping.StreamPing:input_type -> xsvm.StreamPingRequest
	1, // 2: xsvm.Ping.Ping:output_type -> xsvm.PingReply
	3, // 3: xsvm.Ping.StreamPing:output_type -> xsvm.StreamPingReply
	2, // [2:4] is the sub-list for method output_type
	0, // [0:2] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_xsvm_service_proto_init() }
func file_xsvm_service_proto_init() {
	if File_xsvm_service_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_xsvm_service_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*PingRequest); i {
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
		file_xsvm_service_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*PingReply); i {
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
		file_xsvm_service_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*StreamPingRequest); i {
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
		file_xsvm_service_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*StreamPingReply); i {
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
			RawDescriptor: file_xsvm_service_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   4,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_xsvm_service_proto_goTypes,
		DependencyIndexes: file_xsvm_service_proto_depIdxs,
		MessageInfos:      file_xsvm_service_proto_msgTypes,
	}.Build()
	File_xsvm_service_proto = out.File
	file_xsvm_service_proto_rawDesc = nil
	file_xsvm_service_proto_goTypes = nil
	file_xsvm_service_proto_depIdxs = nil
}