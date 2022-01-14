// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

// Interface compliance
var _ Client = &client{}

// Client interface for interacting with the P Chain endpoint
type Client interface {
	// GetHeight returns the current block height of the P Chain
	GetHeight(ctx context.Context) (uint64, error)
	// ExportKey returns the private key corresponding to [address] from [user]'s account
	ExportKey(ctx context.Context, user api.UserPass, address string) (string, error)
	// ImportKey imports the specified [privateKey] to [user]'s keystore
	ImportKey(ctx context.Context, user api.UserPass, address string) (string, error)
	// GetBalance returns the balance of [address] on the P Chain
	GetBalance(ctx context.Context, addrs []string) (*GetBalanceResponse, error)
	// CreateAddress creates a new address for [user]
	CreateAddress(ctx context.Context, user api.UserPass) (string, error)
	// ListAddresses returns an array of platform addresses controlled by [user]
	ListAddresses(ctx context.Context, user api.UserPass) ([]string, error)
	// GetUTXOs returns the byte representation of the UTXOs controlled by [addrs]
	GetUTXOs(
		ctx context.Context,
		addrs []string,
		limit uint32,
		startAddress,
		startUTXOID string,
	) ([][]byte, api.Index, error)
	// GetAtomicUTXOs returns the byte representation of the atomic UTXOs controlled by [addresses]
	// from [sourceChain]
	GetAtomicUTXOs(
		ctx context.Context,
		addrs []string,
		sourceChain string,
		limit uint32,
		startAddress,
		startUTXOID string,
	) ([][]byte, api.Index, error)
	// GetSubnets returns information about the specified subnets
	GetSubnets(context.Context, []ids.ID) ([]APISubnet, error)
	// GetStakingAssetID returns the assetID of the asset used for staking on
	// subnet corresponding to [subnetID]
	GetStakingAssetID(context.Context, ids.ID) (ids.ID, error)
	// GetCurrentValidators returns the list of current validators for subnet with ID [subnetID]
	GetCurrentValidators(ctx context.Context, subnetID ids.ID, nodeIDs []ids.ShortID) ([]interface{}, error)
	// GetPendingValidators returns the list of pending validators for subnet with ID [subnetID]
	GetPendingValidators(ctx context.Context, subnetID ids.ID, nodeIDs []ids.ShortID) ([]interface{}, []interface{}, error)
	// GetCurrentSupply returns an upper bound on the supply of AVAX in the system
	GetCurrentSupply(ctx context.Context) (uint64, error)
	// SampleValidators returns the nodeIDs of a sample of [sampleSize] validators from the current validator set for subnet with ID [subnetID]
	SampleValidators(ctx context.Context, subnetID ids.ID, sampleSize uint16) ([]string, error)
	// AddValidator issues a transaction to add a validator to the primary network
	// and returns the txID
	AddValidator(
		ctx context.Context,
		user api.UserPass,
		from []string,
		changeAddr string,
		rewardAddress,
		nodeID string,
		stakeAmount,
		startTime,
		endTime uint64,
		delegationFeeRate float32,
	) (ids.ID, error)
	// AddDelegator issues a transaction to add a delegator to the primary network
	// and returns the txID
	AddDelegator(
		ctx context.Context,
		user api.UserPass,
		from []string,
		changeAddr string,
		rewardAddress,
		nodeID string,
		stakeAmount,
		startTime,
		endTime uint64,
	) (ids.ID, error)
	// AddSubnetValidator issues a transaction to add validator [nodeID] to subnet
	// with ID [subnetID] and returns the txID
	AddSubnetValidator(
		ctx context.Context,
		user api.UserPass,
		from []string,
		changeAddr string,
		subnetID,
		nodeID string,
		stakeAmount,
		startTime,
		endTime uint64,
	) (ids.ID, error)
	// CreateSubnet issues a transaction to create [subnet] and returns the txID
	CreateSubnet(
		ctx context.Context,
		user api.UserPass,
		from []string,
		changeAddr string,
		controlKeys []string,
		threshold uint32,
	) (ids.ID, error)
	// ExportAVAX issues an ExportTx transaction and returns the txID
	ExportAVAX(
		ctx context.Context,
		user api.UserPass,
		from []string,
		changeAddr string,
		to string,
		amount uint64,
	) (ids.ID, error)
	// ImportAVAX issues an ImportTx transaction and returns the txID
	ImportAVAX(
		ctx context.Context,
		user api.UserPass,
		from []string,
		changeAddr,
		to,
		sourceChain string,
	) (ids.ID, error)
	// CreateBlockchain issues a CreateBlockchain transaction and returns the txID
	CreateBlockchain(
		ctx context.Context,
		user api.UserPass,
		from []string,
		changeAddr string,
		subnetID ids.ID,
		vmID string,
		fxIDs []string,
		name string,
		genesisData []byte,
	) (ids.ID, error)
	// GetBlockchainStatus returns the current status of blockchain with ID: [blockchainID]
	GetBlockchainStatus(ctx context.Context, blockchainID string) (status.BlockchainStatus, error)
	// ValidatedBy returns the ID of the Subnet that validates [blockchainID]
	ValidatedBy(ctx context.Context, blockchainID ids.ID) (ids.ID, error)
	// Validates returns the list of blockchains that are validated by the subnet with ID [subnetID]
	Validates(ctx context.Context, subnetID ids.ID) ([]ids.ID, error)
	// GetBlockchains returns the list of blockchains on the platform
	GetBlockchains(ctx context.Context) ([]APIBlockchain, error)
	// IssueTx issues the transaction and returns its txID
	IssueTx(ctx context.Context, tx []byte) (ids.ID, error)
	// GetTx returns the byte representation of the transaction corresponding to [txID]
	GetTx(ctx context.Context, txID ids.ID) ([]byte, error)
	// GetTxStatus returns the status of the transaction corresponding to [txID]
	GetTxStatus(ctx context.Context, txID ids.ID, includeReason bool) (*GetTxStatusResponse, error)
	// GetStake returns the amount of nAVAX that [addresses] have cumulatively
	// staked on the Primary Network.
	GetStake(ctx context.Context, addrs []string) (*GetStakeReply, error)
	// GetMinStake returns the minimum staking amount in nAVAX for validators
	// and delegators respectively
	GetMinStake(ctx context.Context) (uint64, uint64, error)
	// GetTotalStake returns the total amount (in nAVAX) staked on the network
	GetTotalStake(ctx context.Context) (uint64, error)
	// GetMaxStakeAmount returns the maximum amount of nAVAX staking to the named
	// node during the time period.
	GetMaxStakeAmount(ctx context.Context, subnetID ids.ID, nodeID string, startTime uint64, endTime uint64) (uint64, error)
	// GetRewardUTXOs returns the reward UTXOs for a transaction
	GetRewardUTXOs(context.Context, *api.GetTxArgs) ([][]byte, error)
	// GetTimestamp returns the current chain timestamp
	GetTimestamp(ctx context.Context) (time.Time, error)
	// GetValidatorsAt returns the weights of the validator set of a provided subnet
	// at the specified height.
	GetValidatorsAt(ctx context.Context, subnetID ids.ID, height uint64) (map[string]uint64, error)
}

// Client implementation for interacting with the P Chain endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with the P Chain endpoint
func NewClient(uri string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/P", "platform"),
	}
}

func (c *client) GetHeight(ctx context.Context) (uint64, error) {
	res := &GetHeightResponse{}
	err := c.requester.SendRequest(ctx, "getHeight", struct{}{}, res)
	return uint64(res.Height), err
}

func (c *client) ExportKey(ctx context.Context, user api.UserPass, address string) (string, error) {
	res := &ExportKeyReply{}
	err := c.requester.SendRequest(ctx, "exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  address,
	}, res)
	return res.PrivateKey, err
}

func (c *client) ImportKey(ctx context.Context, user api.UserPass, privateKey string) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res)
	return res.Address, err
}

func (c *client) GetBalance(ctx context.Context, addrs []string) (*GetBalanceResponse, error) {
	res := &GetBalanceResponse{}
	err := c.requester.SendRequest(ctx, "getBalance", &GetBalanceRequest{
		Addresses: addrs,
	}, res)
	return res, err
}

func (c *client) CreateAddress(ctx context.Context, user api.UserPass) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "createAddress", &user, res)
	return res.Address, err
}

func (c *client) ListAddresses(ctx context.Context, user api.UserPass) ([]string, error) {
	res := &api.JSONAddresses{}
	err := c.requester.SendRequest(ctx, "listAddresses", &user, res)
	return res.Addresses, err
}

func (c *client) GetUTXOs(ctx context.Context, addrs []string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error) {
	return c.GetAtomicUTXOs(ctx, addrs, "", limit, startAddress, startUTXOID)
}

func (c *client) GetAtomicUTXOs(ctx context.Context, addrs []string, sourceChain string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error) {
	res := &api.GetUTXOsReply{}
	err := c.requester.SendRequest(ctx, "getUTXOs", &api.GetUTXOsArgs{
		Addresses:   addrs,
		SourceChain: sourceChain,
		Limit:       json.Uint32(limit),
		StartIndex: api.Index{
			Address: startAddress,
			UTXO:    startUTXOID,
		},
		Encoding: formatting.Hex,
	}, res)
	if err != nil {
		return nil, api.Index{}, err
	}

	utxos := make([][]byte, len(res.UTXOs))
	for i, utxo := range res.UTXOs {
		utxoBytes, err := formatting.Decode(res.Encoding, utxo)
		if err != nil {
			return nil, api.Index{}, err
		}
		utxos[i] = utxoBytes
	}
	return utxos, res.EndIndex, nil
}

func (c *client) GetSubnets(ctx context.Context, ids []ids.ID) ([]APISubnet, error) {
	res := &GetSubnetsResponse{}
	err := c.requester.SendRequest(ctx, "getSubnets", &GetSubnetsArgs{
		IDs: ids,
	}, res)
	return res.Subnets, err
}

func (c *client) GetStakingAssetID(ctx context.Context, subnetID ids.ID) (ids.ID, error) {
	res := &GetStakingAssetIDResponse{}
	err := c.requester.SendRequest(ctx, "getStakingAssetID", &GetStakingAssetIDArgs{
		SubnetID: subnetID,
	}, res)
	return res.AssetID, err
}

func (c *client) GetCurrentValidators(ctx context.Context, subnetID ids.ID, nodeIDs []ids.ShortID) ([]interface{}, error) {
	nodeIDsStr := []string{}
	for _, nodeID := range nodeIDs {
		nodeIDsStr = append(nodeIDsStr, nodeID.PrefixedString(constants.NodeIDPrefix))
	}
	res := &GetCurrentValidatorsReply{}
	err := c.requester.SendRequest(ctx, "getCurrentValidators", &GetCurrentValidatorsArgs{
		SubnetID: subnetID,
		NodeIDs:  nodeIDsStr,
	}, res)
	return res.Validators, err
}

func (c *client) GetPendingValidators(ctx context.Context, subnetID ids.ID, nodeIDs []ids.ShortID) ([]interface{}, []interface{}, error) {
	nodeIDsStr := []string{}
	for _, nodeID := range nodeIDs {
		nodeIDsStr = append(nodeIDsStr, nodeID.PrefixedString(constants.NodeIDPrefix))
	}
	res := &GetPendingValidatorsReply{}
	err := c.requester.SendRequest(ctx, "getPendingValidators", &GetPendingValidatorsArgs{
		SubnetID: subnetID,
		NodeIDs:  nodeIDsStr,
	}, res)
	return res.Validators, res.Delegators, err
}

func (c *client) GetCurrentSupply(ctx context.Context) (uint64, error) {
	res := &GetCurrentSupplyReply{}
	err := c.requester.SendRequest(ctx, "getCurrentSupply", struct{}{}, res)
	return uint64(res.Supply), err
}

func (c *client) SampleValidators(ctx context.Context, subnetID ids.ID, sampleSize uint16) ([]string, error) {
	res := &SampleValidatorsReply{}
	err := c.requester.SendRequest(ctx, "sampleValidators", &SampleValidatorsArgs{
		SubnetID: subnetID,
		Size:     json.Uint16(sampleSize),
	}, res)
	return res.Validators, err
}

func (c *client) AddValidator(
	ctx context.Context,
	user api.UserPass,
	from []string,
	changeAddr string,
	rewardAddress,
	nodeID string,
	stakeAmount,
	startTime,
	endTime uint64,
	delegationFeeRate float32,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	jsonStakeAmount := json.Uint64(stakeAmount)
	err := c.requester.SendRequest(ctx, "addValidator", &AddValidatorArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:      user,
			JSONFromAddrs: api.JSONFromAddrs{From: from},
		},
		APIStaker: APIStaker{
			NodeID:      nodeID,
			StakeAmount: &jsonStakeAmount,
			StartTime:   json.Uint64(startTime),
			EndTime:     json.Uint64(endTime),
		},
		RewardAddress:     rewardAddress,
		DelegationFeeRate: json.Float32(delegationFeeRate),
	}, res)
	return res.TxID, err
}

func (c *client) AddDelegator(
	ctx context.Context,
	user api.UserPass,
	from []string,
	changeAddr string,
	rewardAddress,
	nodeID string,
	stakeAmount,
	startTime,
	endTime uint64,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	jsonStakeAmount := json.Uint64(stakeAmount)
	err := c.requester.SendRequest(ctx, "addDelegator", &AddDelegatorArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		}, APIStaker: APIStaker{
			NodeID:      nodeID,
			StakeAmount: &jsonStakeAmount,
			StartTime:   json.Uint64(startTime),
			EndTime:     json.Uint64(endTime),
		},
		RewardAddress: rewardAddress,
	}, res)
	return res.TxID, err
}

func (c *client) AddSubnetValidator(
	ctx context.Context,
	user api.UserPass,
	from []string,
	changeAddr string,
	subnetID,
	nodeID string,
	stakeAmount,
	startTime,
	endTime uint64,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	jsonStakeAmount := json.Uint64(stakeAmount)
	err := c.requester.SendRequest(ctx, "addSubnetValidator", &AddSubnetValidatorArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		APIStaker: APIStaker{
			NodeID:      nodeID,
			StakeAmount: &jsonStakeAmount,
			StartTime:   json.Uint64(startTime),
			EndTime:     json.Uint64(endTime),
		},
		SubnetID: subnetID,
	}, res)
	return res.TxID, err
}

func (c *client) CreateSubnet(
	ctx context.Context,
	user api.UserPass,
	from []string,
	changeAddr string,
	controlKeys []string,
	threshold uint32,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "createSubnet", &CreateSubnetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		APISubnet: APISubnet{
			ControlKeys: controlKeys,
			Threshold:   json.Uint32(threshold),
		},
	}, res)
	return res.TxID, err
}

func (c *client) ExportAVAX(
	ctx context.Context,
	user api.UserPass,
	from []string,
	changeAddr string,
	to string,
	amount uint64,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "exportAVAX", &ExportAVAXArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		To:     to,
		Amount: json.Uint64(amount),
	}, res)
	return res.TxID, err
}

func (c *client) ImportAVAX(
	ctx context.Context,
	user api.UserPass,
	from []string,
	changeAddr,
	to,
	sourceChain string,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "importAVAX", &ImportAVAXArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		To:          to,
		SourceChain: sourceChain,
	}, res)
	return res.TxID, err
}

func (c *client) CreateBlockchain(
	ctx context.Context,
	user api.UserPass,
	from []string,
	changeAddr string,
	subnetID ids.ID,
	vmID string,
	fxIDs []string,
	name string,
	genesisData []byte,
) (ids.ID, error) {
	genesisDataStr, err := formatting.EncodeWithChecksum(formatting.Hex, genesisData)
	if err != nil {
		return ids.ID{}, err
	}

	res := &api.JSONTxID{}
	err = c.requester.SendRequest(ctx, "createBlockchain", &CreateBlockchainArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		SubnetID:    subnetID,
		VMID:        vmID,
		FxIDs:       fxIDs,
		Name:        name,
		GenesisData: genesisDataStr,
		Encoding:    formatting.Hex,
	}, res)
	return res.TxID, err
}

func (c *client) GetBlockchainStatus(ctx context.Context, blockchainID string) (status.BlockchainStatus, error) {
	res := &GetBlockchainStatusReply{}
	err := c.requester.SendRequest(ctx, "getBlockchainStatus", &GetBlockchainStatusArgs{
		BlockchainID: blockchainID,
	}, res)
	return res.Status, err
}

func (c *client) ValidatedBy(ctx context.Context, blockchainID ids.ID) (ids.ID, error) {
	res := &ValidatedByResponse{}
	err := c.requester.SendRequest(ctx, "validatedBy", &ValidatedByArgs{
		BlockchainID: blockchainID,
	}, res)
	return res.SubnetID, err
}

func (c *client) Validates(ctx context.Context, subnetID ids.ID) ([]ids.ID, error) {
	res := &ValidatesResponse{}
	err := c.requester.SendRequest(ctx, "validates", &ValidatesArgs{
		SubnetID: subnetID,
	}, res)
	return res.BlockchainIDs, err
}

func (c *client) GetBlockchains(ctx context.Context) ([]APIBlockchain, error) {
	res := &GetBlockchainsResponse{}
	err := c.requester.SendRequest(ctx, "getBlockchains", struct{}{}, res)
	return res.Blockchains, err
}

func (c *client) IssueTx(ctx context.Context, txBytes []byte) (ids.ID, error) {
	txStr, err := formatting.EncodeWithChecksum(formatting.Hex, txBytes)
	if err != nil {
		return ids.ID{}, err
	}

	res := &api.JSONTxID{}
	err = c.requester.SendRequest(ctx, "issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res)
	return res.TxID, err
}

func (c *client) GetTx(ctx context.Context, txID ids.ID) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.requester.SendRequest(ctx, "getTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: formatting.Hex,
	}, res)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Tx)
}

func (c *client) GetTxStatus(ctx context.Context, txID ids.ID, includeReason bool) (*GetTxStatusResponse, error) {
	res := new(GetTxStatusResponse)
	err := c.requester.SendRequest(ctx, "getTxStatus", &GetTxStatusArgs{
		TxID:          txID,
		IncludeReason: includeReason,
	}, res)
	return res, err
}

func (c *client) GetStake(ctx context.Context, addrs []string) (*GetStakeReply, error) {
	res := new(GetStakeReply)
	err := c.requester.SendRequest(ctx, "getStake", &api.JSONAddresses{
		Addresses: addrs,
	}, res)
	return res, err
}

func (c *client) GetMinStake(ctx context.Context) (uint64, uint64, error) {
	res := new(GetMinStakeReply)
	err := c.requester.SendRequest(ctx, "getMinStake", struct{}{}, res)
	return uint64(res.MinValidatorStake), uint64(res.MinDelegatorStake), err
}

func (c *client) GetTotalStake(ctx context.Context) (uint64, error) {
	res := new(GetTotalStakeReply)
	err := c.requester.SendRequest(ctx, "getTotalStake", struct{}{}, res)
	return uint64(res.Stake), err
}

func (c *client) GetMaxStakeAmount(ctx context.Context, subnetID ids.ID, nodeID string, startTime, endTime uint64) (uint64, error) {
	res := new(GetMaxStakeAmountReply)
	err := c.requester.SendRequest(ctx, "getMaxStakeAmount", &GetMaxStakeAmountArgs{
		SubnetID:  subnetID,
		NodeID:    nodeID,
		StartTime: json.Uint64(startTime),
		EndTime:   json.Uint64(endTime),
	}, res)
	return uint64(res.Amount), err
}

func (c *client) GetRewardUTXOs(ctx context.Context, args *api.GetTxArgs) ([][]byte, error) {
	res := &GetRewardUTXOsReply{}
	err := c.requester.SendRequest(ctx, "getRewardUTXOs", args, res)
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

func (c *client) GetTimestamp(ctx context.Context) (time.Time, error) {
	res := &GetTimestampReply{}
	err := c.requester.SendRequest(ctx, "getTimestamp", struct{}{}, res)
	return res.Timestamp, err
}

func (c *client) GetValidatorsAt(ctx context.Context, subnetID ids.ID, height uint64) (map[string]uint64, error) {
	res := &GetValidatorsAtReply{}
	err := c.requester.SendRequest(ctx, "getValidatorsAt", &GetValidatorsAtArgs{
		SubnetID: subnetID,
		Height:   json.Uint64(height),
	}, res)
	return res.Validators, err
}
