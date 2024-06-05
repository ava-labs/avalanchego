// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             (unknown)
// source: sync/sync.proto

package sync

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	DB_GetMerkleRoot_FullMethodName     = "/sync.DB/GetMerkleRoot"
	DB_Clear_FullMethodName             = "/sync.DB/Clear"
	DB_GetProof_FullMethodName          = "/sync.DB/GetProof"
	DB_GetChangeProof_FullMethodName    = "/sync.DB/GetChangeProof"
	DB_VerifyChangeProof_FullMethodName = "/sync.DB/VerifyChangeProof"
	DB_CommitChangeProof_FullMethodName = "/sync.DB/CommitChangeProof"
)

// DBClient is the client API for DB service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type DBClient interface {
	GetMerkleRoot(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*GetMerkleRootResponse, error)
	Clear(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*emptypb.Empty, error)
	GetProof(ctx context.Context, in *GetProofRequest, opts ...grpc.CallOption) (*GetProofResponse, error)
	GetChangeProof(ctx context.Context, in *GetChangeProofRequest, opts ...grpc.CallOption) (*GetChangeProofResponse, error)
	VerifyChangeProof(ctx context.Context, in *VerifyChangeProofRequest, opts ...grpc.CallOption) (*VerifyChangeProofResponse, error)
	CommitChangeProof(ctx context.Context, in *CommitChangeProofRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

type dBClient struct {
	cc grpc.ClientConnInterface
}

func NewDBClient(cc grpc.ClientConnInterface) DBClient {
	return &dBClient{cc}
}

func (c *dBClient) GetMerkleRoot(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*GetMerkleRootResponse, error) {
	out := new(GetMerkleRootResponse)
	err := c.cc.Invoke(ctx, DB_GetMerkleRoot_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *dBClient) Clear(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, DB_Clear_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *dBClient) GetProof(ctx context.Context, in *GetProofRequest, opts ...grpc.CallOption) (*GetProofResponse, error) {
	out := new(GetProofResponse)
	err := c.cc.Invoke(ctx, DB_GetProof_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *dBClient) GetChangeProof(ctx context.Context, in *GetChangeProofRequest, opts ...grpc.CallOption) (*GetChangeProofResponse, error) {
	out := new(GetChangeProofResponse)
	err := c.cc.Invoke(ctx, DB_GetChangeProof_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *dBClient) VerifyChangeProof(ctx context.Context, in *VerifyChangeProofRequest, opts ...grpc.CallOption) (*VerifyChangeProofResponse, error) {
	out := new(VerifyChangeProofResponse)
	err := c.cc.Invoke(ctx, DB_VerifyChangeProof_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *dBClient) CommitChangeProof(ctx context.Context, in *CommitChangeProofRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, DB_CommitChangeProof_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DBServer is the server API for DB service.
// All implementations must embed UnimplementedDBServer
// for forward compatibility
type DBServer interface {
	GetMerkleRoot(context.Context, *emptypb.Empty) (*GetMerkleRootResponse, error)
	Clear(context.Context, *emptypb.Empty) (*emptypb.Empty, error)
	GetProof(context.Context, *GetProofRequest) (*GetProofResponse, error)
	GetChangeProof(context.Context, *GetChangeProofRequest) (*GetChangeProofResponse, error)
	VerifyChangeProof(context.Context, *VerifyChangeProofRequest) (*VerifyChangeProofResponse, error)
	CommitChangeProof(context.Context, *CommitChangeProofRequest) (*emptypb.Empty, error)
	mustEmbedUnimplementedDBServer()
}

// UnimplementedDBServer must be embedded to have forward compatible implementations.
type UnimplementedDBServer struct {
}

func (UnimplementedDBServer) GetMerkleRoot(context.Context, *emptypb.Empty) (*GetMerkleRootResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetMerkleRoot not implemented")
}
func (UnimplementedDBServer) Clear(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Clear not implemented")
}
func (UnimplementedDBServer) GetProof(context.Context, *GetProofRequest) (*GetProofResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetProof not implemented")
}
func (UnimplementedDBServer) GetChangeProof(context.Context, *GetChangeProofRequest) (*GetChangeProofResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetChangeProof not implemented")
}
func (UnimplementedDBServer) VerifyChangeProof(context.Context, *VerifyChangeProofRequest) (*VerifyChangeProofResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VerifyChangeProof not implemented")
}
func (UnimplementedDBServer) CommitChangeProof(context.Context, *CommitChangeProofRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CommitChangeProof not implemented")
}
func (UnimplementedDBServer) mustEmbedUnimplementedDBServer() {}

// UnsafeDBServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to DBServer will
// result in compilation errors.
type UnsafeDBServer interface {
	mustEmbedUnimplementedDBServer()
}

func RegisterDBServer(s grpc.ServiceRegistrar, srv DBServer) {
	s.RegisterService(&DB_ServiceDesc, srv)
}

func _DB_GetMerkleRoot_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DBServer).GetMerkleRoot(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DB_GetMerkleRoot_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DBServer).GetMerkleRoot(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _DB_Clear_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DBServer).Clear(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DB_Clear_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DBServer).Clear(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _DB_GetProof_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetProofRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DBServer).GetProof(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DB_GetProof_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DBServer).GetProof(ctx, req.(*GetProofRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DB_GetChangeProof_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetChangeProofRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DBServer).GetChangeProof(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DB_GetChangeProof_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DBServer).GetChangeProof(ctx, req.(*GetChangeProofRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DB_VerifyChangeProof_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(VerifyChangeProofRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DBServer).VerifyChangeProof(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DB_VerifyChangeProof_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DBServer).VerifyChangeProof(ctx, req.(*VerifyChangeProofRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DB_CommitChangeProof_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CommitChangeProofRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DBServer).CommitChangeProof(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DB_CommitChangeProof_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DBServer).CommitChangeProof(ctx, req.(*CommitChangeProofRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// DB_ServiceDesc is the grpc.ServiceDesc for DB service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var DB_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "sync.DB",
	HandlerType: (*DBServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetMerkleRoot",
			Handler:    _DB_GetMerkleRoot_Handler,
		},
		{
			MethodName: "Clear",
			Handler:    _DB_Clear_Handler,
		},
		{
			MethodName: "GetProof",
			Handler:    _DB_GetProof_Handler,
		},
		{
			MethodName: "GetChangeProof",
			Handler:    _DB_GetChangeProof_Handler,
		},
		{
			MethodName: "VerifyChangeProof",
			Handler:    _DB_VerifyChangeProof_Handler,
		},
		{
			MethodName: "CommitChangeProof",
			Handler:    _DB_CommitChangeProof_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "sync/sync.proto",
}
