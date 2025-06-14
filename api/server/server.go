package connecthandler

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/api/info"
	v1 "github.com/ava-labs/avalanchego/proto/pb/info/v1"
	v1connect "github.com/ava-labs/avalanchego/proto/pb/info/v1/infov1connect"
	"github.com/bufbuild/connect-go"
	connectgo "github.com/bufbuild/connect-go"
	"google.golang.org/protobuf/types/known/emptypb"
)

// NewConnectInfoService returns a ConnectRPC-compatible InfoServiceHandler
func NewConnectInfoService(svc *info.Info) v1connect.InfoServiceHandler {
	return &connectInfoService{svc: svc}
}

type connectInfoService struct {
	svc *info.Info
}

// GetNodeVersion implements v1connect.InfoServiceHandler
func (c *connectInfoService) GetNodeVersion(
	ctx context.Context,
	req *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNodeVersionReply], error) {
	return connect.NewResponse(&v1.GetNodeVersionReply{
		Version: c.svc.Parameters.Version.String(),
	}), nil
}

// GetNodeID implements v1connect.InfoServiceHandler
func (c *connectInfoService) GetNodeID(
	ctx context.Context,
	req *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNodeIDReply], error) {
	return connect.NewResponse(&v1.GetNodeIDReply{
		NodeID: c.svc.Parameters.NodeID.String(),
	}), nil
}

// IsBootstrapped implements v1connect.InfoServiceHandler
func (c *connectInfoService) IsBootstrapped(
	ctx context.Context,
	req *connectgo.Request[v1.IsBootstrappedResponse],
) (*connectgo.Response[v1.IsBootstrappedResponse], error) {
	return connectgo.NewResponse(&v1.IsBootstrappedResponse{
		IsBootstrapped: false,
	}), nil
}

// GetNodeIP implements v1connect.InfoServiceHandler
func (c *connectInfoService) GetNodeIP(
	ctx context.Context,
	req *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNodeIPReply], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("GetNodeIP not implemented"))
}

// GetNetworkID implements v1connect.InfoServiceHandler
func (c *connectInfoService) GetNetworkID(
	ctx context.Context,
	req *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNetworkIDReply], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("GetNetworkID not implemented"))
}

// GetNetworkName implements v1connect.InfoServiceHandler
func (c *connectInfoService) GetNetworkName(
	ctx context.Context,
	req *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNetworkNameReply], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("GetNetworkName not implemented"))
}

// GetBlockchainID implements v1connect.InfoServiceHandler
func (c *connectInfoService) GetBlockchainID(
	ctx context.Context,
	req *connect.Request[v1.GetBlockchainIDArgs],
) (*connect.Response[v1.GetBlockchainIDReply], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("GetBlockchainID not implemented"))
}

// Peers implements v1connect.InfoServiceHandler
func (c *connectInfoService) Peers(
	ctx context.Context,
	req *connect.Request[v1.PeersArgs],
) (*connect.Response[v1.PeersReply], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("Peers not implemented"))
}

// Upgrades implements v1connect.InfoServiceHandler
func (c *connectInfoService) Upgrades(
	ctx context.Context,
	req *connect.Request[emptypb.Empty],
) (*connect.Response[v1.UpgradesReply], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("Upgrades not implemented"))
}

// Uptime implements v1connect.InfoServiceHandler
func (c *connectInfoService) Uptime(
	ctx context.Context,
	req *connect.Request[emptypb.Empty],
) (*connect.Response[v1.UptimeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("Uptime not implemented"))
}

// GetVMs implements v1connect.InfoServiceHandler
func (c *connectInfoService) GetVMs(
	ctx context.Context,
	req *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetVMsReply], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("GetVMs not implemented"))
}
