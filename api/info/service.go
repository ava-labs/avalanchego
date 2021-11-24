// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
)

var (
	errNoChainProvided = errors.New("argument 'chain' not given")
	errNotValidator    = errors.New("this is not a validator node")
)

// Info is the API service for unprivileged info on a node
type Info struct {
	Parameters
	log           logging.Logger
	networking    network.Network
	chainManager  chains.Manager
	vmManager     vms.Manager
	versionParser version.ApplicationParser
	validators    validators.Set
}

type Parameters struct {
	Version               version.Application
	NodeID                ids.ShortID
	NetworkID             uint32
	TxFee                 uint64
	CreateAssetTxFee      uint64
	CreateSubnetTxFee     uint64
	CreateBlockchainTxFee uint64
}

// NewService returns a new admin API service
func NewService(
	parameters Parameters,
	log logging.Logger,
	chainManager chains.Manager,
	vmManager vms.Manager,
	network network.Network,
	versionParser version.ApplicationParser,
	validators validators.Set,
) (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := newServer.RegisterService(&Info{
		Parameters:    parameters,
		log:           log,
		chainManager:  chainManager,
		vmManager:     vmManager,
		networking:    network,
		versionParser: versionParser,
		validators:    validators,
	}, "info"); err != nil {
		return nil, err
	}
	return &common.HTTPHandler{Handler: newServer}, nil
}

// GetNodeVersionReply are the results from calling GetNodeVersion
type GetNodeVersionReply struct {
	Version         string            `json:"version"`
	DatabaseVersion string            `json:"databaseVersion"`
	GitCommit       string            `json:"gitCommit"`
	VMVersions      map[string]string `json:"vmVersions"`
}

// GetNodeVersion returns the version this node is running
func (service *Info) GetNodeVersion(_ *http.Request, _ *struct{}, reply *GetNodeVersionReply) error {
	service.log.Debug("Info: GetNodeVersion called")

	vmVersions, err := service.vmManager.Versions()
	if err != nil {
		return err
	}

	reply.Version = service.Version.String()
	reply.DatabaseVersion = version.CurrentDatabase.String()
	reply.GitCommit = version.GitCommit
	reply.VMVersions = vmVersions
	return nil
}

// GetNodeIDReply are the results from calling GetNodeID
type GetNodeIDReply struct {
	NodeID string `json:"nodeID"`
}

// GetNodeID returns the node ID of this node
func (service *Info) GetNodeID(_ *http.Request, _ *struct{}, reply *GetNodeIDReply) error {
	service.log.Debug("Info: GetNodeID called")

	reply.NodeID = service.NodeID.PrefixedString(constants.NodeIDPrefix)
	return nil
}

// GetNetworkIDReply are the results from calling GetNetworkID
type GetNetworkIDReply struct {
	NetworkID json.Uint32 `json:"networkID"`
}

// GetNodeIPReply are the results from calling GetNodeIP
type GetNodeIPReply struct {
	IP string `json:"ip"`
}

// GetNodeIP returns the IP of this node
func (service *Info) GetNodeIP(_ *http.Request, _ *struct{}, reply *GetNodeIPReply) error {
	service.log.Debug("Info: GetNodeIP called")

	reply.IP = service.networking.IP().String()
	return nil
}

// GetNetworkID returns the network ID this node is running on
func (service *Info) GetNetworkID(_ *http.Request, _ *struct{}, reply *GetNetworkIDReply) error {
	service.log.Debug("Info: GetNetworkID called")

	reply.NetworkID = json.Uint32(service.NetworkID)
	return nil
}

// GetNetworkNameReply is the result from calling GetNetworkName
type GetNetworkNameReply struct {
	NetworkName string `json:"networkName"`
}

// GetNetworkName returns the network name this node is running on
func (service *Info) GetNetworkName(_ *http.Request, _ *struct{}, reply *GetNetworkNameReply) error {
	service.log.Debug("Info: GetNetworkName called")

	reply.NetworkName = constants.NetworkName(service.NetworkID)
	return nil
}

// GetBlockchainIDArgs are the arguments for calling GetBlockchainID
type GetBlockchainIDArgs struct {
	Alias string `json:"alias"`
}

// GetBlockchainIDReply are the results from calling GetBlockchainID
type GetBlockchainIDReply struct {
	BlockchainID ids.ID `json:"blockchainID"`
}

// GetBlockchainID returns the blockchain ID that resolves the alias that was supplied
func (service *Info) GetBlockchainID(_ *http.Request, args *GetBlockchainIDArgs, reply *GetBlockchainIDReply) error {
	service.log.Debug("Info: GetBlockchainID called")

	bID, err := service.chainManager.Lookup(args.Alias)
	reply.BlockchainID = bID
	return err
}

// PeersArgs are the arguments for calling Peers
type PeersArgs struct {
	NodeIDs []string `json:"nodeIDs"`
}

// PeersReply are the results from calling Peers
type PeersReply struct {
	// Number of elements in [Peers]
	NumPeers json.Uint64 `json:"numPeers"`
	// Each element is a peer
	Peers []network.PeerInfo `json:"peers"`
}

// Peers returns the list of current validators
func (service *Info) Peers(_ *http.Request, args *PeersArgs, reply *PeersReply) error {
	service.log.Debug("Info: Peers called")
	nodeIDs := make([]ids.ShortID, 0, len(args.NodeIDs))
	for _, nodeID := range args.NodeIDs {
		nID, err := ids.ShortFromPrefixedString(nodeID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		nodeIDs = append(nodeIDs, nID)
	}

	reply.Peers = service.networking.Peers(nodeIDs)
	reply.NumPeers = json.Uint64(len(reply.Peers))
	return nil
}

// IsBootstrappedArgs are the arguments for calling IsBootstrapped
type IsBootstrappedArgs struct {
	// Alias of the chain
	// Can also be the string representation of the chain's ID
	Chain string `json:"chain"`
}

// IsBootstrappedResponse are the results from calling IsBootstrapped
type IsBootstrappedResponse struct {
	// True iff the chain exists and is done bootstrapping
	IsBootstrapped bool `json:"isBootstrapped"`
}

// IsBootstrapped returns nil and sets [reply.IsBootstrapped] == true iff [args.Chain] exists and is done bootstrapping
// Returns an error if the chain doesn't exist
func (service *Info) IsBootstrapped(_ *http.Request, args *IsBootstrappedArgs, reply *IsBootstrappedResponse) error {
	service.log.Debug("Info: IsBootstrapped called with chain: %s", args.Chain)

	if args.Chain == "" {
		return errNoChainProvided
	}
	chainID, err := service.chainManager.Lookup(args.Chain)
	if err != nil {
		return fmt.Errorf("there is no chain with alias/ID '%s'", args.Chain)
	}
	reply.IsBootstrapped = service.chainManager.IsBootstrapped(chainID)
	return nil
}

// UptimeResponse are the results from calling Uptime
type UptimeResponse struct {
	// RewardingStakePercentage shows what percent of network stake thinks we're
	// above the uptime requirement.
	RewardingStakePercentage json.Float64 `json:"rewardingStakePercentage"`

	// WeightedAveragePercentage is the average perceived uptime of this node,
	// weighted by stake.
	// Note that this is different from RewardingStakePercentage, which shows
	// the percent of the network stake that thinks this node is above the
	// uptime requirement. WeightedAveragePercentage is weighted by uptime.
	// i.e If uptime requirement is 85 and a peer reports 40 percent it will be
	// counted (40*weight) in WeightedAveragePercentage but not in
	// RewardingStakePercentage since 40 < 85
	WeightedAveragePercentage json.Float64 `json:"weightedAveragePercentage"`
}

func (service *Info) Uptime(_ *http.Request, _ *struct{}, reply *UptimeResponse) error {
	service.log.Debug("Info: Uptime called")
	result, isValidator := service.networking.NodeUptime()
	if !isValidator {
		return errNotValidator
	}
	reply.WeightedAveragePercentage = json.Float64(result.WeightedAveragePercentage)
	reply.RewardingStakePercentage = json.Float64(result.RewardingStakePercentage)
	return nil
}

type GetTxFeeResponse struct {
	TxFee json.Uint64 `json:"txFee"`
	// TODO: remove [CreationTxFee] after enough time for dependencies to update
	CreationTxFee         json.Uint64 `json:"creationTxFee"`
	CreateAssetTxFee      json.Uint64 `json:"createAssetTxFee"`
	CreateSubnetTxFee     json.Uint64 `json:"createSubnetTxFee"`
	CreateBlockchainTxFee json.Uint64 `json:"createBlockchainTxFee"`
}

// GetTxFee returns the transaction fee in nAVAX.
func (service *Info) GetTxFee(_ *http.Request, args *struct{}, reply *GetTxFeeResponse) error {
	reply.TxFee = json.Uint64(service.TxFee)
	reply.CreationTxFee = json.Uint64(service.CreateAssetTxFee)
	reply.CreateAssetTxFee = json.Uint64(service.CreateAssetTxFee)
	reply.CreateSubnetTxFee = json.Uint64(service.CreateSubnetTxFee)
	reply.CreateBlockchainTxFee = json.Uint64(service.CreateBlockchainTxFee)
	return nil
}
