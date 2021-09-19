// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
)

var errNoChainProvided = errors.New("argument 'chain' not given")

// Info is the API service for unprivileged info on a node
type Info struct {
	version               version.Application
	nodeID                ids.ShortID
	networkID             uint32
	log                   logging.Logger
	networking            network.Network
	chainManager          chains.Manager
	vmManager             vms.Manager
	txFee                 uint64
	createAssetTxFee      uint64
	createSubnetTxFee     uint64
	createBlockchainTxFee uint64
}

// NewService returns a new admin API service
func NewService(
	log logging.Logger,
	version version.Application,
	nodeID ids.ShortID,
	networkID uint32,
	chainManager chains.Manager,
	vmManager vms.Manager,
	peers network.Network,
	txFee uint64,
	createAssetTxFee uint64,
	createSubnetTxFee uint64,
	createBlockchainTxFee uint64,
) (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := newServer.RegisterService(&Info{
		version:               version,
		nodeID:                nodeID,
		networkID:             networkID,
		log:                   log,
		chainManager:          chainManager,
		vmManager:             vmManager,
		networking:            peers,
		txFee:                 txFee,
		createAssetTxFee:      createAssetTxFee,
		createSubnetTxFee:     createSubnetTxFee,
		createBlockchainTxFee: createBlockchainTxFee,
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

	reply.Version = service.version.String()
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

	reply.NodeID = service.nodeID.PrefixedString(constants.NodeIDPrefix)
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

	reply.NetworkID = json.Uint32(service.networkID)
	return nil
}

// GetNetworkNameReply is the result from calling GetNetworkName
type GetNetworkNameReply struct {
	NetworkName string `json:"networkName"`
}

// GetNetworkName returns the network name this node is running on
func (service *Info) GetNetworkName(_ *http.Request, _ *struct{}, reply *GetNetworkNameReply) error {
	service.log.Debug("Info: GetNetworkName called")

	reply.NetworkName = constants.NetworkName(service.networkID)
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
	Peers []network.PeerID `json:"peers"`
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
	reply.TxFee = json.Uint64(service.txFee)
	reply.CreationTxFee = json.Uint64(service.createAssetTxFee)
	reply.CreateAssetTxFee = json.Uint64(service.createAssetTxFee)
	reply.CreateSubnetTxFee = json.Uint64(service.createSubnetTxFee)
	reply.CreateBlockchainTxFee = json.Uint64(service.createBlockchainTxFee)
	return nil
}
