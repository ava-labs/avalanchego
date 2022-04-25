// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	addressconverter "github.com/ava-labs/avalanchego/utils/formatting/addressconverter"
)

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

	delegationFeeKey = "delegationFee"
	uptimeKey        = "uptime"
	connectedKey     = "connected"

	delegatorsKey = "delegators"
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

func getStringValFromMapIntf(vdrDgtMap map[string]interface{}, key string) (string, error) {
	vIntf, ok := vdrDgtMap[key]
	if !ok {
		return "", fmt.Errorf("key %q not found in map", key)
	}
	vStr := vIntf.(string)
	if !ok {
		return "", fmt.Errorf("expected string for %q got %T", key, vIntf)
	}
	return vStr, nil
}

func getTimeValFromMapIntf(vdrDgtMap map[string]interface{}, key string) (time.Time, error) {
	timeStr, err := getStringValFromMapIntf(vdrDgtMap, key)
	if err != nil {
		return time.Time{}, err
	}
	timeUint, err := strconv.ParseUint(timeStr, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("could not parse %q from %q to uint: %w", key, timeStr, err)
	}
	return time.Unix(int64(timeUint), 0), nil
}

func getUint64ValFromMapIntf(vdrDgtMap map[string]interface{}, key string) (uint64, error) {
	vStr, err := getStringValFromMapIntf(vdrDgtMap, key)
	if err != nil {
		return 0, err
	}
	vUint64, err := strconv.ParseUint(vStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("could not parse %q from %q to uint: %w", key, vStr, err)
	}
	return vUint64, nil
}

func getUint32ValFromMapIntf(vdrDgtMap map[string]interface{}, key string) (uint32, error) {
	vStr, err := getStringValFromMapIntf(vdrDgtMap, key)
	if err != nil {
		return 0, err
	}
	vUint64, err := strconv.ParseUint(vStr, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("could not parse %q from %q to uint: %w", key, vStr, err)
	}
	return uint32(vUint64), nil
}

func getFloat32ValFromMapIntf(vdrDgtMap map[string]interface{}, key string) (float32, error) {
	vStr, err := getStringValFromMapIntf(vdrDgtMap, key)
	if err != nil {
		return 0, err
	}
	vFloat64, err := strconv.ParseFloat(vStr, 32)
	if err != nil {
		return 0, fmt.Errorf("could not parse %q from %q to uint: %w", key, vStr, err)
	}
	return float32(vFloat64), nil
}

func getBoolValFromMapIntf(vdrDgtMap map[string]interface{}, key string) (bool, error) {
	vIntf, ok := vdrDgtMap[key]
	if !ok {
		return false, fmt.Errorf("key %q not found in map", key)
	}
	vBool := vIntf.(bool)
	if !ok {
		return false, fmt.Errorf("expected bool for %q got %T", key, vIntf)
	}
	return vBool, nil
}

func getClientOwnerFromMapIntf(vdrDgtMap map[string]interface{}) (ClientOwner, error) {
	var err error
	rewardOwnerIntf, ok := vdrDgtMap[rewardOwnerKey]
	if !ok {
		return ClientOwner{}, fmt.Errorf("key %q not found in map", rewardOwnerKey)
	}
	rewardOwnerMap, ok := rewardOwnerIntf.(map[string]interface{})
	if !ok {
		return ClientOwner{}, fmt.Errorf("expected map[string]interface{} for %q got %T", rewardOwnerKey, rewardOwnerIntf)
	}
	clientOwner := ClientOwner{}
	clientOwner.Locktime, err = getUint64ValFromMapIntf(rewardOwnerMap, locktimeKey)
	if err != nil {
		return ClientOwner{}, err
	}
	clientOwner.Threshold, err = getUint32ValFromMapIntf(rewardOwnerMap, thresholdKey)
	if err != nil {
		return ClientOwner{}, err
	}
	addressesIntf, ok := rewardOwnerMap[addressesKey]
	if !ok {
		return ClientOwner{}, fmt.Errorf("key %q not found in map", addressesKey)
	}
	addressesSliceIntf, ok := addressesIntf.([]interface{})
	if !ok {
		return ClientOwner{}, fmt.Errorf("expected []interface{} for %q got %T", addressesKey, addressesIntf)
	}
	clientOwner.Addresses = make([]ids.ShortID, len(addressesSliceIntf))
	for i, addressIntf := range addressesSliceIntf {
		address, ok := addressIntf.(string)
		if !ok {
			return ClientOwner{}, fmt.Errorf("expected string got %T", addressIntf)
		}
		clientOwner.Addresses[i], err = addressconverter.ParseAddressToID(address)
		if err != nil {
			return ClientOwner{}, err
		}
	}
	return clientOwner, nil
}

func getClientStakersFromMapIntf(stakersSliceIntf []interface{}, subnetID ids.ID, isValidator bool) ([]ClientStaker, error) {
	var err error
	clientStakers := make([]ClientStaker, len(stakersSliceIntf))
	for i, stakerMapIntf := range stakersSliceIntf {
		stakerMap, ok := stakerMapIntf.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected map[string]interface{} got %T", stakerMapIntf)
		}
		clientStakers[i], err = getClientStakerFromMapIntf(stakerMap, subnetID, isValidator)
		if err != nil {
			return nil, err
		}
	}
	return clientStakers, nil
}

func getClientStakerFromMapIntf(vdrDgtMap map[string]interface{}, subnetID ids.ID, isValidator bool) (ClientStaker, error) {
	var err error
	clientStaker := ClientStaker{}
	txIDStr, err := getStringValFromMapIntf(vdrDgtMap, txIDKey)
	if err != nil {
		return ClientStaker{}, err
	}
	clientStaker.TxID, err = ids.FromString(txIDStr)
	if err != nil {
		return ClientStaker{}, fmt.Errorf("couldn't parse %q from %q to ids.ID: %w", txIDKey, txIDStr, err)
	}
	clientStaker.StartTime, err = getTimeValFromMapIntf(vdrDgtMap, startTimeKey)
	if err != nil {
		return ClientStaker{}, err
	}
	clientStaker.EndTime, err = getTimeValFromMapIntf(vdrDgtMap, endTimeKey)
	if err != nil {
		return ClientStaker{}, err
	}
	if subnetID == constants.PrimaryNetworkID {
		clientStaker.StakeAmount, err = getUint64ValFromMapIntf(vdrDgtMap, stakeAmountKey)
		if err != nil {
			return ClientStaker{}, err
		}
	}
	nodeIDStr, err := getStringValFromMapIntf(vdrDgtMap, nodeIDKey)
	if err != nil {
		return ClientStaker{}, err
	}
	clientStaker.NodeID, err = ids.ShortFromPrefixedString(nodeIDStr, constants.NodeIDPrefix)
	if err != nil {
		return ClientStaker{}, err
	}
	clientStaker.RewardOwner, err = getClientOwnerFromMapIntf(vdrDgtMap)
	if err != nil {
		return ClientStaker{}, err
	}
	clientStaker.PotentialReward, err = getUint64ValFromMapIntf(vdrDgtMap, potentialRewardKey)
	if err != nil {
		return ClientStaker{}, err
	}
	if isValidator {
		if subnetID != constants.PrimaryNetworkID {
			clientStaker.Weight, err = getUint64ValFromMapIntf(vdrDgtMap, weightKey)
			if err != nil {
				return ClientStaker{}, err
			}
		}
		clientStaker.DelegationFee, err = getFloat32ValFromMapIntf(vdrDgtMap, delegationFeeKey)
		if err != nil {
			return ClientStaker{}, err
		}
		clientStaker.Uptime, err = getFloat32ValFromMapIntf(vdrDgtMap, uptimeKey)
		if err != nil {
			return ClientStaker{}, err
		}
		clientStaker.Connected, err = getBoolValFromMapIntf(vdrDgtMap, connectedKey)
		if err != nil {
			return ClientStaker{}, err
		}
		dgtsIntf, ok := vdrDgtMap[delegatorsKey]
		if !ok {
			return ClientStaker{}, fmt.Errorf("key %q not found in map", delegatorsKey)
		}
		if dgtsIntf != nil {
			dgts, ok := dgtsIntf.([]interface{})
			if !ok {
				return ClientStaker{}, fmt.Errorf("expected []interface{} for %q got %T", delegatorsKey, dgtsIntf)
			}
			clientStaker.Delegators, err = getClientStakersFromMapIntf(dgts, subnetID, false)
			if err != nil {
				return ClientStaker{}, err
			}
		}
	}
	return clientStaker, nil
}
