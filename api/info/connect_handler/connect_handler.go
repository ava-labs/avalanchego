// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connecthandler

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade"

	v1 "github.com/ava-labs/avalanchego/proto/pb/info/v1"
)

// NewConnectInfoService returns a ConnectRPC-compatible InfoServiceHandler
// that delegates calls to the existing Info implementation
func NewConnectInfoService(info *info.Info) *ConnectInfoService {
	return &ConnectInfoService{
		Info: info,
	}
}

type ConnectInfoService struct {
	*info.Info
}

// GetNodeVersion returns the semantic version, database version, RPC protocol version,
// Git commit hash, and the list of VM versions this node is running
func (s *ConnectInfoService) GetNodeVersion(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNodeVersionReply], error) {
	var jsonReply info.GetNodeVersionReply
	if err := s.Info.GetNodeVersion(nil, nil, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Convert VM versions map to protobuf format
	vmVersions := make(map[string]string)
	for id, version := range jsonReply.VMVersions {
		vmVersions[id] = version
	}

	reply := &v1.GetNodeVersionReply{
		Version:            jsonReply.Version,
		DatabaseVersion:    jsonReply.DatabaseVersion,
		RpcProtocolVersion: uint32(jsonReply.RPCProtocolVersion),
		GitCommit:          jsonReply.GitCommit,
		VmVersions:         vmVersions,
	}

	return connect.NewResponse(reply), nil
}

// GetNodeID returns this node's unique identifier and proof-of-possession bytes
func (s *ConnectInfoService) GetNodeID(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNodeIDReply], error) {
	var jsonReply info.GetNodeIDReply
	if err := s.Info.GetNodeID(nil, nil, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	nodePOP := []byte{}
	if jsonReply.NodePOP != nil {
		// Use Marshal to serialize ProofOfPossession to bytes
		// MarshalJSON is not ideal here. Ideally, we would use a binary serialization method (MarshalBinary, Marshal, etc.)
		var err error
		nodePOP, err = jsonReply.NodePOP.MarshalJSON()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to marshal NodePOP: %w", err))
		}
	}

	reply := &v1.GetNodeIDReply{
		NodeId:  jsonReply.NodeID.String(),
		NodePop: nodePOP,
	}

	return connect.NewResponse(reply), nil
}

// GetNodeIP returns the primary IP address this node uses for P2P networking.\
func (s *ConnectInfoService) GetNodeIP(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNodeIPReply], error) {
	var jsonReply info.GetNodeIPReply
	if err := s.Info.GetNodeIP(nil, nil, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	reply := &v1.GetNodeIPReply{
		Ip: jsonReply.IP.String(),
	}

	return connect.NewResponse(reply), nil
}

// GetNetworkID returns the numeric ID of the Avalanche network this node is connected to
func (s *ConnectInfoService) GetNetworkID(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNetworkIDReply], error) {
	var jsonReply info.GetNetworkIDReply
	if err := s.Info.GetNetworkID(nil, nil, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	reply := &v1.GetNetworkIDReply{
		NetworkId: uint32(jsonReply.NetworkID),
	}

	return connect.NewResponse(reply), nil
}

// GetNetworkName returns the name of the network
func (s *ConnectInfoService) GetNetworkName(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetNetworkNameReply], error) {
	var jsonReply info.GetNetworkNameReply
	if err := s.Info.GetNetworkName(nil, nil, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	reply := &v1.GetNetworkNameReply{
		NetworkName: jsonReply.NetworkName,
	}

	return connect.NewResponse(reply), nil
}

// GetBlockchainID maps an ID string to its canonical chain ID
func (s *ConnectInfoService) GetBlockchainID(
	_ context.Context,
	req *connect.Request[v1.GetBlockchainIDArgs],
) (*connect.Response[v1.GetBlockchainIDReply], error) {
	jsonArgs := info.GetBlockchainIDArgs{
		Alias: req.Msg.Alias,
	}

	var jsonReply info.GetBlockchainIDReply
	if err := s.Info.GetBlockchainID(nil, &jsonArgs, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	reply := &v1.GetBlockchainIDReply{
		BlockchainId: jsonReply.BlockchainID.String(),
	}

	return connect.NewResponse(reply), nil
}

// Peers returns metadata (IP, nodeID, version, uptimes, subnets, etc.) for the given peer node IDs
func (s *ConnectInfoService) Peers(
	_ context.Context,
	req *connect.Request[v1.PeersArgs],
) (*connect.Response[v1.PeersReply], error) {
	nodeIDs := make([]ids.NodeID, 0, len(req.Msg.NodeIds))
	for _, nodeIDStr := range req.Msg.NodeIds {
		nodeID, err := ids.NodeIDFromString(nodeIDStr)
		if err != nil {
			return nil, connect.NewError(
				connect.CodeInvalidArgument, fmt.Errorf("invalid nodeID %s: %w", nodeIDStr, err))
		}
		nodeIDs = append(nodeIDs, nodeID)
	}

	jsonArgs := info.PeersArgs{
		NodeIDs: nodeIDs,
	}

	var jsonReply info.PeersReply
	if err := s.Info.Peers(nil, &jsonArgs, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	peers := make([]*v1.PeerInfo, 0, len(jsonReply.Peers))
	for _, peer := range jsonReply.Peers {
		// Convert TrackedSubnets (set.Set[ids.ID]) to []string
		trackedSubnetsIDs := peer.TrackedSubnets.List()
		trackedSubnets := make([]string, len(trackedSubnetsIDs))
		for i, id := range trackedSubnetsIDs {
			trackedSubnets[i] = id.String()
		}

		benched := make([]string, len(peer.Benched))
		copy(benched, peer.Benched)

		peers = append(peers, &v1.PeerInfo{
			Ip:             peer.IP.String(),
			PublicIp:       peer.PublicIP.String(),
			NodeId:         peer.ID.String(),
			Version:        peer.Version,
			LastSent:       formatTime(peer.LastSent),
			LastReceived:   formatTime(peer.LastReceived),
			Benched:        benched,
			ObservedUptime: uint32(peer.ObservedUptime),
			TrackedSubnets: trackedSubnets,
		})
	}

	reply := &v1.PeersReply{
		NumPeers: uint32(jsonReply.NumPeers),
		Peers:    peers,
	}

	return connect.NewResponse(reply), nil
}

// Helper function to format time
func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

// IsBootstrapped returns whether the named chain has finished its bootstrap process on this node
func (s *ConnectInfoService) IsBootstrapped(
	_ context.Context,
	req *connect.Request[v1.IsBootstrappedArgs],
) (*connect.Response[v1.IsBootstrappedResponse], error) {
	// Use the chain from the request
	jsonArgs := info.IsBootstrappedArgs{
		Chain: req.Msg.Chain,
	}

	var jsonReply info.IsBootstrappedResponse
	if err := s.Info.IsBootstrapped(nil, &jsonArgs, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	reply := &v1.IsBootstrappedResponse{
		IsBootstrapped: jsonReply.IsBootstrapped,
	}

	return connect.NewResponse(reply), nil
}

// Upgrades returns all the scheduled upgrade activation times and parameters for this node
func (s *ConnectInfoService) Upgrades(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[v1.UpgradesReply], error) {
	var config upgrade.Config
	if err := s.Info.Upgrades(nil, nil, &config); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	reply := &v1.UpgradesReply{
		ApricotPhase1Time:            timestamppb.New(config.ApricotPhase1Time),
		ApricotPhase2Time:            timestamppb.New(config.ApricotPhase2Time),
		ApricotPhase3Time:            timestamppb.New(config.ApricotPhase3Time),
		ApricotPhase4Time:            timestamppb.New(config.ApricotPhase4Time),
		ApricotPhase4MinPChainHeight: config.ApricotPhase4MinPChainHeight,
		ApricotPhase5Time:            timestamppb.New(config.ApricotPhase5Time),
		ApricotPhasePre6Time:         timestamppb.New(config.ApricotPhasePre6Time),
		ApricotPhase6Time:            timestamppb.New(config.ApricotPhase6Time),
		ApricotPhasePost6Time:        timestamppb.New(config.ApricotPhasePost6Time),
		BanffTime:                    timestamppb.New(config.BanffTime),
		CortinaTime:                  timestamppb.New(config.CortinaTime),
		CortinaXChainStopVertexId:    config.CortinaXChainStopVertexID.String(),
		DurangoTime:                  timestamppb.New(config.DurangoTime),
		EtnaTime:                     timestamppb.New(config.EtnaTime),
		FortunaTime:                  timestamppb.New(config.FortunaTime),
		GraniteTime:                  timestamppb.New(config.GraniteTime),
	}

	return connect.NewResponse(reply), nil
}

// Uptime returns this node's uptime metrics (rewarding stake %, weighted average %, etc.)
func (s *ConnectInfoService) Uptime(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[v1.UptimeResponse], error) {
	var jsonReply info.UptimeResponse
	if err := s.Info.Uptime(nil, nil, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	reply := &v1.UptimeResponse{
		RewardingStakePercentage:  float64(jsonReply.RewardingStakePercentage),
		WeightedAveragePercentage: float64(jsonReply.WeightedAveragePercentage),
	}

	return connect.NewResponse(reply), nil
}

// GetVMs returns a map of VM IDs to their known aliases, plus FXs information
func (s *ConnectInfoService) GetVMs(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[v1.GetVMsReply], error) {
	var jsonReply info.GetVMsReply
	if err := s.Info.GetVMs(nil, nil, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Convert the VM map from JSON-RPC format to protobuf format
	vms := make(map[string]*v1.VMAliases)
	for vmID, aliases := range jsonReply.VMs {
		vms[vmID.String()] = &v1.VMAliases{
			Aliases: aliases,
		}
	}

	// Convert the FXs map from JSON-RPC format to protobuf format
	fxs := make(map[string]string)
	for fxID, name := range jsonReply.Fxs {
		fxs[fxID.String()] = name
	}

	reply := &v1.GetVMsReply{
		Vms: vms,
		Fxs: fxs,
	}

	return connect.NewResponse(reply), nil
}
