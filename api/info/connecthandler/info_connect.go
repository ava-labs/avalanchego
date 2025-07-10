// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connecthandler

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/info/v1/infov1connect"
	"github.com/ava-labs/avalanchego/upgrade"

	v1 "github.com/ava-labs/avalanchego/proto/pb/info/v1"
)

var _ infov1connect.InfoServiceHandler = (*connectInfoService)(nil)

type connectInfoService struct {
	*info.Info
}

// NewConnectInfoService returns a ConnectRPC handler for the Info API
func NewConnectInfoService(info *info.Info) infov1connect.InfoServiceHandler {
	return &connectInfoService{
		Info: info,
	}
}

// GetNodeVersion returns the semantic version, database version, RPC protocol version,
// Git commit hash, and the list of VM versions this node is running
func (c *connectInfoService) GetNodeVersion(
	_ context.Context,
	_ *connect.Request[v1.GetNodeVersionRequest],
) (*connect.Response[v1.GetNodeVersionResponse], error) {
	var jsonResponse info.GetNodeVersionReply
	if err := c.Info.GetNodeVersion(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	// Convert VM versions map to protobuf format
	vmVersions := make(map[string]string)
	for id, version := range jsonResponse.VMVersions {
		vmVersions[id] = version
	}

	response := &v1.GetNodeVersionResponse{
		Version:            jsonResponse.Version,
		DatabaseVersion:    jsonResponse.DatabaseVersion,
		RpcProtocolVersion: uint32(jsonResponse.RPCProtocolVersion),
		GitCommit:          jsonResponse.GitCommit,
		VmVersions:         vmVersions,
	}

	return connect.NewResponse(response), nil
}

// GetNodeID returns this node's unique identifier and proof-of-possession bytes
func (c *connectInfoService) GetNodeID(
	_ context.Context,
	_ *connect.Request[v1.GetNodeIDRequest],
) (*connect.Response[v1.GetNodeIDResponse], error) {
	var jsonResponse info.GetNodeIDReply
	if err := c.Info.GetNodeID(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	nodePOP := []byte{}
	if jsonResponse.NodePOP != nil {
		p := jsonResponse.NodePOP
		nodePOP = make([]byte, len(p.PublicKey)+len(p.ProofOfPossession))
		copy(nodePOP[0:len(p.PublicKey)], p.PublicKey[:])
		copy(nodePOP[len(p.PublicKey):], p.ProofOfPossession[:])
	}

	response := &v1.GetNodeIDResponse{
		NodeId:  jsonResponse.NodeID.String(),
		NodePop: nodePOP,
	}

	return connect.NewResponse(response), nil
}

// GetNodeIP returns the primary IP address this node uses for P2P networking.\
func (c *connectInfoService) GetNodeIP(
	_ context.Context,
	_ *connect.Request[v1.GetNodeIPRequest],
) (*connect.Response[v1.GetNodeIPResponse], error) {
	var jsonResponse info.GetNodeIPReply
	if err := c.Info.GetNodeIP(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	response := &v1.GetNodeIPResponse{
		Ip: jsonResponse.IP.String(),
	}

	return connect.NewResponse(response), nil
}

// GetNetworkID returns the numeric ID of the Avalanche network this node is connected to
func (c *connectInfoService) GetNetworkID(
	_ context.Context,
	_ *connect.Request[v1.GetNetworkIDRequest],
) (*connect.Response[v1.GetNetworkIDResponse], error) {
	var jsonResponse info.GetNetworkIDReply
	if err := c.Info.GetNetworkID(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	response := &v1.GetNetworkIDResponse{
		NetworkId: uint32(jsonResponse.NetworkID),
	}

	return connect.NewResponse(response), nil
}

// GetNetworkName returns the name of the network
func (c *connectInfoService) GetNetworkName(
	_ context.Context,
	_ *connect.Request[v1.GetNetworkNameRequest],
) (*connect.Response[v1.GetNetworkNameResponse], error) {
	var jsonResponse info.GetNetworkNameReply
	if err := c.Info.GetNetworkName(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	response := &v1.GetNetworkNameResponse{
		NetworkName: jsonResponse.NetworkName,
	}

	return connect.NewResponse(response), nil
}

// GetBlockchainID maps an ID string to its canonical chain ID
func (c *connectInfoService) GetBlockchainID(
	_ context.Context,
	request *connect.Request[v1.GetBlockchainIDRequest],
) (*connect.Response[v1.GetBlockchainIDResponse], error) {
	jsonRequest := info.GetBlockchainIDArgs{
		Alias: request.Msg.Alias,
	}

	var jsonResponse info.GetBlockchainIDReply
	if err := c.Info.GetBlockchainID(nil, &jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	response := &v1.GetBlockchainIDResponse{
		BlockchainId: jsonResponse.BlockchainID.String(),
	}

	return connect.NewResponse(response), nil
}

// Peers returns metadata (IP, nodeID, version, uptimes, subnets, etc.) for the given peer node IDs
func (c *connectInfoService) Peers(
	_ context.Context,
	request *connect.Request[v1.PeersRequest],
) (*connect.Response[v1.PeersResponse], error) {
	nodeIDs := make([]ids.NodeID, 0, len(request.Msg.NodeIds))
	for _, nodeIDStr := range request.Msg.NodeIds {
		nodeID, err := ids.NodeIDFromString(nodeIDStr)
		if err != nil {
			return nil, connect.NewError(
				connect.CodeInvalidArgument, fmt.Errorf("invalid nodeID %s: %w", nodeIDStr, err))
		}
		nodeIDs = append(nodeIDs, nodeID)
	}

	jsonRequest := info.PeersArgs{
		NodeIDs: nodeIDs,
	}

	var jsonResponse info.PeersReply
	if err := c.Info.Peers(nil, &jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	peers := make([]*v1.PeerInfo, 0, len(jsonResponse.Peers))
	for _, peer := range jsonResponse.Peers {
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

	response := &v1.PeersResponse{
		NumPeers: uint32(jsonResponse.NumPeers),
		Peers:    peers,
	}

	return connect.NewResponse(response), nil
}

// formatTime formats a time.Time object to a string in "YYYY-MM-DDThh:mm:ssZ" format
func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

// IsBootstrapped returns whether the named chain has finished its bootstrap process on this node
func (c *connectInfoService) IsBootstrapped(
	_ context.Context,
	request *connect.Request[v1.IsBootstrappedRequest],
) (*connect.Response[v1.IsBootstrappedResponse], error) {
	// Use the chain from the request
	jsonRequest := info.IsBootstrappedArgs{
		Chain: request.Msg.Chain,
	}

	var jsonResponse info.IsBootstrappedResponse
	if err := c.Info.IsBootstrapped(nil, &jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	response := &v1.IsBootstrappedResponse{
		IsBootstrapped: jsonResponse.IsBootstrapped,
	}

	return connect.NewResponse(response), nil
}

// Upgrades returns all the scheduled upgrade activation times and parameters for this node
func (c *connectInfoService) Upgrades(
	_ context.Context,
	_ *connect.Request[v1.UpgradesRequest],
) (*connect.Response[v1.UpgradesResponse], error) {
	var config upgrade.Config
	if err := c.Info.Upgrades(nil, nil, &config); err != nil {
		return nil, err
	}

	response := &v1.UpgradesResponse{
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

	return connect.NewResponse(response), nil
}

// Uptime returns this node's uptime metrics (rewarding stake %, weighted average %, etc.)
func (c *connectInfoService) Uptime(
	_ context.Context,
	_ *connect.Request[v1.UptimeRequest],
) (*connect.Response[v1.UptimeResponse], error) {
	var jsonResponse info.UptimeResponse
	if err := c.Info.Uptime(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	response := &v1.UptimeResponse{
		RewardingStakePercentage:  float64(jsonResponse.RewardingStakePercentage),
		WeightedAveragePercentage: float64(jsonResponse.WeightedAveragePercentage),
	}

	return connect.NewResponse(response), nil
}

// GetVMs returns a map of VM IDs to their known aliases, plus FXs information
func (c *connectInfoService) GetVMs(
	_ context.Context,
	_ *connect.Request[v1.GetVMsRequest],
) (*connect.Response[v1.GetVMsResponse], error) {
	var jsonResponse info.GetVMsReply
	if err := c.Info.GetVMs(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	// Convert the VM map from JSON-RPC format to protobuf format
	vms := make(map[string]*v1.VMAliases)
	for vmID, aliases := range jsonResponse.VMs {
		vms[vmID.String()] = &v1.VMAliases{
			Aliases: aliases,
		}
	}

	// Convert the FXs map from JSON-RPC format to protobuf format
	fxs := make(map[string]string)
	for fxID, name := range jsonResponse.Fxs {
		fxs[fxID.String()] = name
	}

	response := &v1.GetVMsResponse{
		Vms: vms,
		Fxs: fxs,
	}

	return connect.NewResponse(response), nil
}
