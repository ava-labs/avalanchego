// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	addressconverter "github.com/ava-labs/avalanchego/utils/formatting/addressconverter"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

const chainIDAlias = "P"

// Interface compliance
var _ Client = &client{}

// Client interface for interacting with the P Chain endpoint
type Client interface {
	// GetHeight returns the current block height of the P Chain
	GetHeight(ctx context.Context, options ...rpc.Option) (uint64, error)
	// ExportKey returns the private key corresponding to [address] from [user]'s account
	ExportKey(ctx context.Context, user api.UserPass, address ids.ShortID, options ...rpc.Option) (string, error)
	// ImportKey imports the specified [privateKey] to [user]'s keystore
	ImportKey(ctx context.Context, user api.UserPass, privateKey string, options ...rpc.Option) (ids.ShortID, error)
	// GetBalance returns the balance of [addrs] on the P Chain
	GetBalance(ctx context.Context, addrs []ids.ShortID, options ...rpc.Option) (*GetBalanceResponse, error)
	// CreateAddress creates a new address for [user]
	CreateAddress(ctx context.Context, user api.UserPass, options ...rpc.Option) (ids.ShortID, error)
	// ListAddresses returns an array of platform addresses controlled by [user]
	ListAddresses(ctx context.Context, user api.UserPass, options ...rpc.Option) ([]ids.ShortID, error)
	// GetUTXOs returns the byte representation of the UTXOs controlled by [addrs]
	GetUTXOs(
		ctx context.Context,
		addrs []ids.ShortID,
		limit uint32,
		startAddress ids.ShortID,
		startUTXOID ids.ID,
		options ...rpc.Option,
	) ([][]byte, ids.ShortID, ids.ID, error)
	// GetAtomicUTXOs returns the byte representation of the atomic UTXOs controlled by [addrs]
	// from [sourceChain]
	GetAtomicUTXOs(
		ctx context.Context,
		addrs []ids.ShortID,
		sourceChain string,
		limit uint32,
		startAddress ids.ShortID,
		startUTXOID ids.ID,
		options ...rpc.Option,
	) ([][]byte, ids.ShortID, ids.ID, error)
	// GetSubnets returns information about the specified subnets
	GetSubnets(context.Context, []ids.ID, ...rpc.Option) ([]ClientSubnet, error)
	// GetStakingAssetID returns the assetID of the asset used for staking on
	// subnet corresponding to [subnetID]
	GetStakingAssetID(context.Context, ids.ID, ...rpc.Option) (ids.ID, error)
	// GetCurrentValidators returns the list of current validators for subnet with ID [subnetID]
	GetCurrentValidators(ctx context.Context, subnetID ids.ID, nodeIDs []ids.ShortID, options ...rpc.Option) ([]interface{}, error)
	// GetPendingValidators returns the list of pending validators for subnet with ID [subnetID]
	GetPendingValidators(ctx context.Context, subnetID ids.ID, nodeIDs []ids.ShortID, options ...rpc.Option) ([]interface{}, []interface{}, error)
	// GetCurrentSupply returns an upper bound on the supply of AVAX in the system
	GetCurrentSupply(ctx context.Context, options ...rpc.Option) (uint64, error)
	// SampleValidators returns the nodeIDs of a sample of [sampleSize] validators from the current validator set for subnet with ID [subnetID]
	SampleValidators(ctx context.Context, subnetID ids.ID, sampleSize uint16, options ...rpc.Option) ([]ids.ShortID, error)
	// AddValidator issues a transaction to add a validator to the primary network
	// and returns the txID
	AddValidator(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		rewardAddress ids.ShortID,
		nodeID ids.ShortID,
		stakeAmount,
		startTime,
		endTime uint64,
		delegationFeeRate float32,
		options ...rpc.Option,
	) (ids.ID, error)
	// AddDelegator issues a transaction to add a delegator to the primary network
	// and returns the txID
	AddDelegator(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		rewardAddress ids.ShortID,
		nodeID ids.ShortID,
		stakeAmount,
		startTime,
		endTime uint64,
		options ...rpc.Option,
	) (ids.ID, error)
	// AddSubnetValidator issues a transaction to add validator [nodeID] to subnet
	// with ID [subnetID] and returns the txID
	AddSubnetValidator(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		subnetID ids.ID,
		nodeID ids.ShortID,
		stakeAmount,
		startTime,
		endTime uint64,
		options ...rpc.Option,
	) (ids.ID, error)
	// CreateSubnet issues a transaction to create [subnet] and returns the txID
	CreateSubnet(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		controlKeys []ids.ShortID,
		threshold uint32,
		options ...rpc.Option,
	) (ids.ID, error)
	// ExportAVAX issues an ExportTx transaction and returns the txID
	ExportAVAX(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		to ids.ShortID,
		toChainIDAlias string,
		amount uint64,
		options ...rpc.Option,
	) (ids.ID, error)
	// ImportAVAX issues an ImportTx transaction and returns the txID
	ImportAVAX(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		to ids.ShortID,
		sourceChain string,
		options ...rpc.Option,
	) (ids.ID, error)
	// CreateBlockchain issues a CreateBlockchain transaction and returns the txID
	CreateBlockchain(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		subnetID ids.ID,
		vmID ids.ID,
		fxIDs []ids.ID,
		name string,
		genesisData []byte,
		options ...rpc.Option,
	) (ids.ID, error)
	// GetBlockchainStatus returns the current status of blockchain with ID: [blockchainID]
	GetBlockchainStatus(ctx context.Context, blockchainID ids.ID, options ...rpc.Option) (status.BlockchainStatus, error)
	// ValidatedBy returns the ID of the Subnet that validates [blockchainID]
	ValidatedBy(ctx context.Context, blockchainID ids.ID, options ...rpc.Option) (ids.ID, error)
	// Validates returns the list of blockchains that are validated by the subnet with ID [subnetID]
	Validates(ctx context.Context, subnetID ids.ID, options ...rpc.Option) ([]ids.ID, error)
	// GetBlockchains returns the list of blockchains on the platform
	GetBlockchains(ctx context.Context, options ...rpc.Option) ([]APIBlockchain, error)
	// IssueTx issues the transaction and returns its txID
	IssueTx(ctx context.Context, tx []byte, options ...rpc.Option) (ids.ID, error)
	// GetTx returns the byte representation of the transaction corresponding to [txID]
	GetTx(ctx context.Context, txID ids.ID, options ...rpc.Option) ([]byte, error)
	// GetTxStatus returns the status of the transaction corresponding to [txID]
	GetTxStatus(ctx context.Context, txID ids.ID, includeReason bool, options ...rpc.Option) (*GetTxStatusResponse, error)
	// AwaitTxDecided polls [GetTxStatus] until a status is returned that
	// implies the tx may be decided.
	AwaitTxDecided(
		ctx context.Context,
		txID ids.ID,
		includeReason bool,
		freq time.Duration,
		options ...rpc.Option,
	) (*GetTxStatusResponse, error)
	// GetStake returns the amount of nAVAX that [addrs] have cumulatively
	// staked on the Primary Network.
	GetStake(ctx context.Context, addrs []ids.ShortID, options ...rpc.Option) (uint64, [][]byte, error)
	// GetMinStake returns the minimum staking amount in nAVAX for validators
	// and delegators respectively
	GetMinStake(ctx context.Context, options ...rpc.Option) (uint64, uint64, error)
	// GetTotalStake returns the total amount (in nAVAX) staked on the network
	GetTotalStake(ctx context.Context, options ...rpc.Option) (uint64, error)
	// GetMaxStakeAmount returns the maximum amount of nAVAX staking to the named
	// node during the time period.
	GetMaxStakeAmount(
		ctx context.Context,
		subnetID ids.ID,
		nodeID ids.ShortID,
		startTime uint64,
		endTime uint64,
		options ...rpc.Option,
	) (uint64, error)
	// GetRewardUTXOs returns the reward UTXOs for a transaction
	GetRewardUTXOs(context.Context, *api.GetTxArgs, ...rpc.Option) ([][]byte, error)
	// GetTimestamp returns the current chain timestamp
	GetTimestamp(ctx context.Context, options ...rpc.Option) (time.Time, error)
	// GetValidatorsAt returns the weights of the validator set of a provided subnet
	// at the specified height.
	GetValidatorsAt(ctx context.Context, subnetID ids.ID, height uint64, options ...rpc.Option) (map[ids.ShortID]uint64, error)
	// GetBlock returns the block with the given id.
	GetBlock(ctx context.Context, blockID ids.ID, options ...rpc.Option) ([]byte, error)
}

// Client implementation for interacting with the P Chain endpoint
type client struct {
	requester rpc.EndpointRequester
	// used for address ID -> string conversion
	hrp string
}

// NewClient returns a Client for interacting with the P Chain endpoint
func NewClient(uri string, networkID uint32) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/P", "platform"),
		hrp:       constants.GetHRP(networkID),
	}
}

func (c *client) GetHeight(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &GetHeightResponse{}
	err := c.requester.SendRequest(ctx, "getHeight", struct{}{}, res, options...)
	return uint64(res.Height), err
}

func (c *client) ExportKey(ctx context.Context, user api.UserPass, address ids.ShortID, options ...rpc.Option) (string, error) {
	res := &ExportKeyReply{}
	addressStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, address[:])
	if err != nil {
		return "", err
	}
	err = c.requester.SendRequest(ctx, "exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  addressStr,
	}, res, options...)
	return res.PrivateKey, err
}

func (c *client) ImportKey(ctx context.Context, user api.UserPass, privateKey string, options ...rpc.Option) (ids.ShortID, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res, options...)
	if err != nil {
		return ids.ShortID{}, err
	}
	return addressconverter.ParseAddressToID(res.Address)
}

func (c *client) GetBalance(ctx context.Context, addrs []ids.ShortID, options ...rpc.Option) (*GetBalanceResponse, error) {
	res := &GetBalanceResponse{}
	addrsStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, addrs)
	if err != nil {
		return nil, err
	}
	err = c.requester.SendRequest(ctx, "getBalance", &GetBalanceRequest{
		Addresses: addrsStr,
	}, res, options...)
	return res, err
}

func (c *client) CreateAddress(ctx context.Context, user api.UserPass, options ...rpc.Option) (ids.ShortID, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "createAddress", &user, res, options...)
	if err != nil {
		return ids.ShortID{}, err
	}
	return addressconverter.ParseAddressToID(res.Address)
}

func (c *client) ListAddresses(ctx context.Context, user api.UserPass, options ...rpc.Option) ([]ids.ShortID, error) {
	res := &api.JSONAddresses{}
	err := c.requester.SendRequest(ctx, "listAddresses", &user, res, options...)
	if err != nil {
		return nil, err
	}
	return addressconverter.ParseAddressesToID(res.Addresses)
}

func (c *client) GetUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	limit uint32,
	startAddress ids.ShortID,
	startUTXOID ids.ID,
	options ...rpc.Option,
) ([][]byte, ids.ShortID, ids.ID, error) {
	return c.GetAtomicUTXOs(ctx, addrs, "", limit, startAddress, startUTXOID, options...)
}

func (c *client) GetAtomicUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	sourceChain string,
	limit uint32,
	startAddress ids.ShortID,
	startUTXOID ids.ID,
	options ...rpc.Option,
) ([][]byte, ids.ShortID, ids.ID, error) {
	res := &api.GetUTXOsReply{}
	addrsStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, addrs)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, err
	}
	startAddressStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, startAddress[:])
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, err
	}
	startUTXOIDStr := startUTXOID.String()
	err = c.requester.SendRequest(ctx, "getUTXOs", &api.GetUTXOsArgs{
		Addresses:   addrsStr,
		SourceChain: sourceChain,
		Limit:       json.Uint32(limit),
		StartIndex: api.Index{
			Address: startAddressStr,
			UTXO:    startUTXOIDStr,
		},
		Encoding: formatting.Hex,
	}, res, options...)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, err
	}

	utxos := make([][]byte, len(res.UTXOs))
	for i, utxo := range res.UTXOs {
		utxoBytes, err := formatting.Decode(res.Encoding, utxo)
		if err != nil {
			return nil, ids.ShortID{}, ids.Empty, err
		}
		utxos[i] = utxoBytes
	}
	endAddr, err := addressconverter.ParseAddressToID(res.EndIndex.Address)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, err
	}
	endUTXOID, err := ids.FromString(res.EndIndex.UTXO)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, err
	}
	return utxos, endAddr, endUTXOID, nil
}

// ClientSubnet is a representation of a subnet used in client methods
type ClientSubnet struct {
	// ID of the subnet
	ID ids.ID
	// Each element of [ControlKeys] the address of a public key.
	// A transaction to add a validator to this subnet requires
	// signatures from [Threshold] of these keys to be valid.
	ControlKeys []ids.ShortID
	Threshold   uint32
}

func (c *client) GetSubnets(ctx context.Context, ids []ids.ID, options ...rpc.Option) ([]ClientSubnet, error) {
	res := &GetSubnetsResponse{}
	err := c.requester.SendRequest(ctx, "getSubnets", &GetSubnetsArgs{
		IDs: ids,
	}, res, options...)
	if err != nil {
		return nil, err
	}
	subnets := make([]ClientSubnet, len(res.Subnets))
	for i, APIsubnet := range res.Subnets {
		subnets[i].ID = APIsubnet.ID
		subnets[i].ControlKeys, err = addressconverter.ParseAddressesToID(APIsubnet.ControlKeys)
		if err != nil {
			return nil, err
		}
		subnets[i].Threshold = uint32(APIsubnet.Threshold)
	}
	return subnets, err
}

func (c *client) GetStakingAssetID(ctx context.Context, subnetID ids.ID, options ...rpc.Option) (ids.ID, error) {
	res := &GetStakingAssetIDResponse{}
	err := c.requester.SendRequest(ctx, "getStakingAssetID", &GetStakingAssetIDArgs{
		SubnetID: subnetID,
	}, res, options...)
	return res.AssetID, err
}

const (
	txIDKey        = "txID"
	startTimeKey   = "startTime"
	endTimeKey     = "endTime"
	stakeAmountKey = "stakeAmount"
	nodeIDKey      = "nodeID"
	weightKey      = "weight"

	rewardOwnerKey     = "rewardOwner"
	locktimeKey        = "locktime"
	thresholdKey       = "threshold"
	addressesKey       = "addresses"
	potentialRewardKey = "potentialReward"
)

type ClientOwner struct {
	Locktime  uint64
	Threshold uint32
	Addresses []ids.ShortID
}

type ClientStaker struct {
	TxID            ids.ID
	StartTime       time.Time
	EndTime         time.Time
	StakeAmount     uint64
	NodeID          ids.ShortID
	Weight          uint64
	RewardOwner     ClientOwner
	PotentialReward uint64
	DelegationFee   float32
	Uptime          float32
	Connected       bool
	Delegators      []ClientStaker
}

func getClientStaker(vdrDgtMap map[string]interface{}) (ClientStaker, error) {
	var err error
	clientStaker := ClientStaker{}
	txIDIntf, ok := vdrDgtMap[txIDKey]
	if !ok {
		return ClientStaker{}, fmt.Errorf("key %q not found in map", txIDKey)
	}
	txIDStr := txIDIntf.(string)
	if !ok {
		return ClientStaker{}, fmt.Errorf("expected string for %q got %T", txIDKey, txIDIntf)
	}
	clientStaker.TxID, err = ids.FromString(txIDStr)
	if err != nil {
		return ClientStaker{}, fmt.Errorf("couldn't parse %q from %q to ids.ID: %w", txIDKey, txIDStr, err)
	}
	startTimeIntf, ok := vdrDgtMap[startTimeKey]
	if !ok {
		return ClientStaker{}, fmt.Errorf("key %q not found in map", startTimeKey)
	}
	startTimeStr, ok := startTimeIntf.(string)
	if !ok {
		return ClientStaker{}, fmt.Errorf("expected string for %q got %T", startTimeKey, startTimeIntf)
	}
	startTimeUint, err := strconv.ParseUint(startTimeStr, 10, 64)
	if err != nil {
		return ClientStaker{}, fmt.Errorf("could not parse %q from %q to uint: %w", startTimeKey, startTimeStr, err)
	}
	clientStaker.StartTime = time.Unix(int64(startTimeUint), 0)
	endTimeIntf, ok := vdrDgtMap[endTimeKey]
	if !ok {
		return ClientStaker{}, fmt.Errorf("key %q not found in map", endTimeKey)
	}
	endTimeStr, ok := endTimeIntf.(string)
	if !ok {
		return ClientStaker{}, fmt.Errorf("expected string for %q got %T", endTimeKey, endTimeIntf)
	}
	endTimeUint, err := strconv.ParseUint(endTimeStr, 10, 64)
	if err != nil {
		return ClientStaker{}, fmt.Errorf("could not parse %q from %q to uint: %w", endTimeKey, endTimeStr, err)
	}
	clientStaker.EndTime = time.Unix(int64(endTimeUint), 0)
	stakeAmountIntf, ok := vdrDgtMap[stakeAmountKey]
	if !ok {
		return ClientStaker{}, fmt.Errorf("key %q not found in map", stakeAmountKey)
	}
	stakeAmountStr, ok := stakeAmountIntf.(string)
	if !ok {
		return ClientStaker{}, fmt.Errorf("expected string for %q got %T", stakeAmountKey, stakeAmountIntf)
	}
	clientStaker.StakeAmount, err = strconv.ParseUint(stakeAmountStr, 10, 64)
	if err != nil {
		return ClientStaker{}, fmt.Errorf("could not parse %q from %q to uint: %w", stakeAmountKey, stakeAmountStr, err)
	}
	nodeIDIntf, ok := vdrDgtMap[nodeIDKey]
	if !ok {
		return ClientStaker{}, fmt.Errorf("key %q not found in map", nodeIDKey)
	}
	nodeIDStr, ok := nodeIDIntf.(string)
	if !ok {
		return ClientStaker{}, fmt.Errorf("expected string for %q got %T", nodeIDKey, nodeIDIntf)
	}
	clientStaker.NodeID, err = ids.ShortFromPrefixedString(nodeIDStr, constants.NodeIDPrefix)
	if err != nil {
		return ClientStaker{}, err
	}
	potentialRewardIntf, ok := vdrDgtMap[potentialRewardKey]
	if !ok {
		return ClientStaker{}, fmt.Errorf("key %q not found in map", potentialRewardKey)
	}
	potentialRewardStr, ok := potentialRewardIntf.(string)
	if !ok {
		return ClientStaker{}, fmt.Errorf("expected string for %q got %T", potentialRewardKey, potentialRewardIntf)
	}
	clientStaker.PotentialReward, err = strconv.ParseUint(potentialRewardStr, 10, 64)
	if err != nil {
		return ClientStaker{}, fmt.Errorf("could not parse %q from %q to uint: %w", potentialRewardKey, potentialRewardStr, err)
	}
	return clientStaker, nil
}

func (c *client) GetCurrentValidators(
	ctx context.Context,
	subnetID ids.ID,
	nodeIDs []ids.ShortID,
	options ...rpc.Option,
) ([]interface{}, error) {
	nodeIDsStr := []string{}
	for _, nodeID := range nodeIDs {
		nodeIDsStr = append(nodeIDsStr, nodeID.PrefixedString(constants.NodeIDPrefix))
	}
	res := &GetCurrentValidatorsReply{}
	err := c.requester.SendRequest(ctx, "getCurrentValidators", &GetCurrentValidatorsArgs{
		SubnetID: subnetID,
		NodeIDs:  nodeIDsStr,
	}, res, options...)
	return res.Validators, err
}

func (c *client) GetPendingValidators(
	ctx context.Context,
	subnetID ids.ID,
	nodeIDs []ids.ShortID,
	options ...rpc.Option,
) ([]interface{}, []interface{}, error) {
	nodeIDsStr := []string{}
	for _, nodeID := range nodeIDs {
		nodeIDsStr = append(nodeIDsStr, nodeID.PrefixedString(constants.NodeIDPrefix))
	}
	res := &GetPendingValidatorsReply{}
	err := c.requester.SendRequest(ctx, "getPendingValidators", &GetPendingValidatorsArgs{
		SubnetID: subnetID,
		NodeIDs:  nodeIDsStr,
	}, res, options...)
	return res.Validators, res.Delegators, err
}

func (c *client) GetCurrentSupply(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &GetCurrentSupplyReply{}
	err := c.requester.SendRequest(ctx, "getCurrentSupply", struct{}{}, res, options...)
	return uint64(res.Supply), err
}

func (c *client) SampleValidators(ctx context.Context, subnetID ids.ID, sampleSize uint16, options ...rpc.Option) ([]ids.ShortID, error) {
	res := &SampleValidatorsReply{}
	err := c.requester.SendRequest(ctx, "sampleValidators", &SampleValidatorsArgs{
		SubnetID: subnetID,
		Size:     json.Uint16(sampleSize),
	}, res, options...)
	if err != nil {
		return nil, err
	}
	validators := make([]ids.ShortID, len(res.Validators))
	for i, validatorStr := range res.Validators {
		validators[i], err = ids.ShortFromPrefixedString(validatorStr, constants.NodeIDPrefix)
		if err != nil {
			return nil, err
		}
	}
	return validators, nil
}

func (c *client) AddValidator(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	rewardAddress ids.ShortID,
	nodeID ids.ShortID,
	stakeAmount,
	startTime,
	endTime uint64,
	delegationFeeRate float32,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	fromStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, from)
	if err != nil {
		return ids.Empty, err
	}
	rewardAddressStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, rewardAddress[:])
	if err != nil {
		return ids.Empty, err
	}
	jsonStakeAmount := json.Uint64(stakeAmount)
	err = c.requester.SendRequest(ctx, "addValidator", &AddValidatorArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:      user,
			JSONFromAddrs: api.JSONFromAddrs{From: fromStr},
		},
		APIStaker: APIStaker{
			NodeID:      nodeID.PrefixedString(constants.NodeIDPrefix),
			StakeAmount: &jsonStakeAmount,
			StartTime:   json.Uint64(startTime),
			EndTime:     json.Uint64(endTime),
		},
		RewardAddress:     rewardAddressStr,
		DelegationFeeRate: json.Float32(delegationFeeRate),
	}, res, options...)
	return res.TxID, err
}

func (c *client) AddDelegator(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	rewardAddress ids.ShortID,
	nodeID ids.ShortID,
	stakeAmount,
	startTime,
	endTime uint64,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	fromStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, from)
	if err != nil {
		return ids.Empty, err
	}
	changeAddrStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, changeAddr[:])
	if err != nil {
		return ids.Empty, err
	}
	rewardAddressStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, rewardAddress[:])
	if err != nil {
		return ids.Empty, err
	}
	jsonStakeAmount := json.Uint64(stakeAmount)
	err = c.requester.SendRequest(ctx, "addDelegator", &AddDelegatorArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: fromStr},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddrStr},
		}, APIStaker: APIStaker{
			NodeID:      nodeID.PrefixedString(constants.NodeIDPrefix),
			StakeAmount: &jsonStakeAmount,
			StartTime:   json.Uint64(startTime),
			EndTime:     json.Uint64(endTime),
		},
		RewardAddress: rewardAddressStr,
	}, res, options...)
	return res.TxID, err
}

func (c *client) AddSubnetValidator(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	subnetID ids.ID,
	nodeID ids.ShortID,
	stakeAmount,
	startTime,
	endTime uint64,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	fromStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, from)
	if err != nil {
		return ids.Empty, err
	}
	changeAddrStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, changeAddr[:])
	if err != nil {
		return ids.Empty, err
	}
	jsonStakeAmount := json.Uint64(stakeAmount)
	err = c.requester.SendRequest(ctx, "addSubnetValidator", &AddSubnetValidatorArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: fromStr},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddrStr},
		},
		APIStaker: APIStaker{
			NodeID:      nodeID.PrefixedString(constants.NodeIDPrefix),
			StakeAmount: &jsonStakeAmount,
			StartTime:   json.Uint64(startTime),
			EndTime:     json.Uint64(endTime),
		},
		SubnetID: subnetID.String(),
	}, res, options...)
	return res.TxID, err
}

func (c *client) CreateSubnet(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	controlKeys []ids.ShortID,
	threshold uint32,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	fromStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, from)
	if err != nil {
		return ids.Empty, err
	}
	changeAddrStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, changeAddr[:])
	if err != nil {
		return ids.Empty, err
	}
	controlKeysStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, controlKeys)
	if err != nil {
		return ids.Empty, err
	}
	err = c.requester.SendRequest(ctx, "createSubnet", &CreateSubnetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: fromStr},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddrStr},
		},
		APISubnet: APISubnet{
			ControlKeys: controlKeysStr,
			Threshold:   json.Uint32(threshold),
		},
	}, res, options...)
	return res.TxID, err
}

func (c *client) ExportAVAX(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	to ids.ShortID,
	toChainIDAlias string,
	amount uint64,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	fromStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, from)
	if err != nil {
		return ids.Empty, err
	}
	changeAddrStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, changeAddr[:])
	if err != nil {
		return ids.Empty, err
	}
	toStr, err := formatting.FormatAddress(toChainIDAlias, c.hrp, to[:])
	if err != nil {
		return ids.Empty, err
	}
	err = c.requester.SendRequest(ctx, "exportAVAX", &ExportAVAXArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: fromStr},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddrStr},
		},
		To:     toStr,
		Amount: json.Uint64(amount),
	}, res, options...)
	return res.TxID, err
}

func (c *client) ImportAVAX(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	to ids.ShortID,
	sourceChain string,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	fromStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, from)
	if err != nil {
		return ids.Empty, err
	}
	changeAddrStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, changeAddr[:])
	if err != nil {
		return ids.Empty, err
	}
	toStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, to[:])
	if err != nil {
		return ids.Empty, err
	}
	err = c.requester.SendRequest(ctx, "importAVAX", &ImportAVAXArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: fromStr},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddrStr},
		},
		To:          toStr,
		SourceChain: sourceChain,
	}, res, options...)
	return res.TxID, err
}

func (c *client) CreateBlockchain(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	subnetID ids.ID,
	vmID ids.ID,
	fxIDs []ids.ID,
	name string,
	genesisData []byte,
	options ...rpc.Option,
) (ids.ID, error) {
	genesisDataStr, err := formatting.EncodeWithChecksum(formatting.Hex, genesisData)
	if err != nil {
		return ids.ID{}, err
	}

	res := &api.JSONTxID{}
	fromStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, from)
	if err != nil {
		return ids.Empty, err
	}
	changeAddrStr, err := formatting.FormatAddress(chainIDAlias, c.hrp, changeAddr[:])
	if err != nil {
		return ids.Empty, err
	}
	fxIDsStr := make([]string, len(fxIDs))
	for i, fxID := range fxIDs {
		fxIDsStr[i] = fxID.String()
	}
	err = c.requester.SendRequest(ctx, "createBlockchain", &CreateBlockchainArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: fromStr},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddrStr},
		},
		SubnetID:    subnetID,
		VMID:        vmID.String(),
		FxIDs:       fxIDsStr,
		Name:        name,
		GenesisData: genesisDataStr,
		Encoding:    formatting.Hex,
	}, res, options...)
	return res.TxID, err
}

func (c *client) GetBlockchainStatus(ctx context.Context, blockchainID ids.ID, options ...rpc.Option) (status.BlockchainStatus, error) {
	res := &GetBlockchainStatusReply{}
	err := c.requester.SendRequest(ctx, "getBlockchainStatus", &GetBlockchainStatusArgs{
		BlockchainID: blockchainID.String(),
	}, res, options...)
	return res.Status, err
}

func (c *client) ValidatedBy(ctx context.Context, blockchainID ids.ID, options ...rpc.Option) (ids.ID, error) {
	res := &ValidatedByResponse{}
	err := c.requester.SendRequest(ctx, "validatedBy", &ValidatedByArgs{
		BlockchainID: blockchainID,
	}, res, options...)
	return res.SubnetID, err
}

func (c *client) Validates(ctx context.Context, subnetID ids.ID, options ...rpc.Option) ([]ids.ID, error) {
	res := &ValidatesResponse{}
	err := c.requester.SendRequest(ctx, "validates", &ValidatesArgs{
		SubnetID: subnetID,
	}, res, options...)
	return res.BlockchainIDs, err
}

func (c *client) GetBlockchains(ctx context.Context, options ...rpc.Option) ([]APIBlockchain, error) {
	res := &GetBlockchainsResponse{}
	err := c.requester.SendRequest(ctx, "getBlockchains", struct{}{}, res, options...)
	return res.Blockchains, err
}

func (c *client) IssueTx(ctx context.Context, txBytes []byte, options ...rpc.Option) (ids.ID, error) {
	txStr, err := formatting.EncodeWithChecksum(formatting.Hex, txBytes)
	if err != nil {
		return ids.ID{}, err
	}

	res := &api.JSONTxID{}
	err = c.requester.SendRequest(ctx, "issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res, options...)
	return res.TxID, err
}

func (c *client) GetTx(ctx context.Context, txID ids.ID, options ...rpc.Option) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.requester.SendRequest(ctx, "getTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: formatting.Hex,
	}, res, options...)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Tx)
}

func (c *client) GetTxStatus(ctx context.Context, txID ids.ID, includeReason bool, options ...rpc.Option) (*GetTxStatusResponse, error) {
	res := new(GetTxStatusResponse)
	err := c.requester.SendRequest(ctx, "getTxStatus", &GetTxStatusArgs{
		TxID:          txID,
		IncludeReason: includeReason,
	}, res, options...)
	return res, err
}

func (c *client) AwaitTxDecided(ctx context.Context, txID ids.ID, includeReason bool, freq time.Duration, options ...rpc.Option) (*GetTxStatusResponse, error) {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		res, err := c.GetTxStatus(ctx, txID, includeReason, options...)
		if err == nil {
			switch res.Status {
			case status.Committed, status.Aborted, status.Dropped:
				return res, nil
			}
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *client) GetStake(ctx context.Context, addrs []ids.ShortID, options ...rpc.Option) (uint64, [][]byte, error) {
	res := new(GetStakeReply)
	addrsStr, err := addressconverter.FormatAddressesFromID(chainIDAlias, c.hrp, addrs)
	if err != nil {
		return 0, nil, err
	}
	err = c.requester.SendRequest(ctx, "getStake", &GetStakeArgs{
		JSONAddresses: api.JSONAddresses{
			Addresses: addrsStr,
		},
		Encoding: formatting.Hex,
	}, res, options...)
	if err != nil {
		return 0, nil, err
	}

	outputs := make([][]byte, len(res.Outputs))
	for i, outputStr := range res.Outputs {
		output, err := formatting.Decode(res.Encoding, outputStr)
		if err != nil {
			return 0, nil, err
		}
		outputs[i] = output
	}
	return uint64(res.Staked), outputs, err
}

func (c *client) GetMinStake(ctx context.Context, options ...rpc.Option) (uint64, uint64, error) {
	res := new(GetMinStakeReply)
	err := c.requester.SendRequest(ctx, "getMinStake", struct{}{}, res, options...)
	return uint64(res.MinValidatorStake), uint64(res.MinDelegatorStake), err
}

func (c *client) GetTotalStake(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := new(GetTotalStakeReply)
	err := c.requester.SendRequest(ctx, "getTotalStake", struct{}{}, res, options...)
	return uint64(res.Stake), err
}

func (c *client) GetMaxStakeAmount(ctx context.Context, subnetID ids.ID, nodeID ids.ShortID, startTime, endTime uint64, options ...rpc.Option) (uint64, error) {
	res := new(GetMaxStakeAmountReply)
	err := c.requester.SendRequest(ctx, "getMaxStakeAmount", &GetMaxStakeAmountArgs{
		SubnetID:  subnetID,
		NodeID:    nodeID.PrefixedString(constants.NodeIDPrefix),
		StartTime: json.Uint64(startTime),
		EndTime:   json.Uint64(endTime),
	}, res, options...)
	return uint64(res.Amount), err
}

func (c *client) GetRewardUTXOs(ctx context.Context, args *api.GetTxArgs, options ...rpc.Option) ([][]byte, error) {
	res := &GetRewardUTXOsReply{}
	err := c.requester.SendRequest(ctx, "getRewardUTXOs", args, res, options...)
	if err != nil {
		return nil, err
	}
	utxos := make([][]byte, len(res.UTXOs))
	for i, utxoStr := range res.UTXOs {
		utxoBytes, err := formatting.Decode(res.Encoding, utxoStr)
		if err != nil {
			return nil, err
		}
		utxos[i] = utxoBytes
	}
	return utxos, err
}

func (c *client) GetTimestamp(ctx context.Context, options ...rpc.Option) (time.Time, error) {
	res := &GetTimestampReply{}
	err := c.requester.SendRequest(ctx, "getTimestamp", struct{}{}, res, options...)
	return res.Timestamp, err
}

func (c *client) GetValidatorsAt(ctx context.Context, subnetID ids.ID, height uint64, options ...rpc.Option) (map[ids.ShortID]uint64, error) {
	res := &GetValidatorsAtReply{}
	err := c.requester.SendRequest(ctx, "getValidatorsAt", &GetValidatorsAtArgs{
		SubnetID: subnetID,
		Height:   json.Uint64(height),
	}, res, options...)
	validators := map[ids.ShortID]uint64{}
	for validatorStr, validatorWeight := range res.Validators {
		validatorID, err := ids.ShortFromPrefixedString(validatorStr, constants.NodeIDPrefix)
		if err != nil {
			return nil, err
		}
		validators[validatorID] = validatorWeight
	}
	return validators, err
}

func (c *client) GetBlock(ctx context.Context, blockID ids.ID, options ...rpc.Option) ([]byte, error) {
	response := &api.FormattedBlock{}
	if err := c.requester.SendRequest(ctx, "getBlock", &api.GetBlockArgs{
		BlockID:  blockID,
		Encoding: formatting.Hex,
	}, response, options...); err != nil {
		return nil, err
	}

	return formatting.Decode(response.Encoding, response.Block)
}
