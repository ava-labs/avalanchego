// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type CaminoClient interface {
	// GetConfiguration returns genesis information of the primary network
	GetConfiguration(ctx context.Context, options ...rpc.Option) (*GetConfigurationReply, error)

	// GetMultisigAlias returns the alias definition of the given multisig address
	GetMultisigAlias(ctx context.Context, multisigAddress string, options ...rpc.Option) (*GetMultisigAliasReply, error)

	GetAllDepositOffers(ctx context.Context, getAllDepositOffersArgs *GetAllDepositOffersArgs, options ...rpc.Option) (*GetAllDepositOffersReply, error)

	GetRegisteredShortIDLink(ctx context.Context, addrStr ids.ShortID, options ...rpc.Option) (string, error)
	GetLastAcceptedBlock(ctx context.Context, encoding formatting.Encoding, options ...rpc.Option) (any, error)
	GetBlockAtHeight(ctx context.Context, height uint32, encoding formatting.Encoding, options ...rpc.Option) (any, error)
	GetClaimables(ctx context.Context, owners []*secp256k1fx.OutputOwners, options ...rpc.Option) ([]*state.Claimable, error)
}

func (c *client) GetConfiguration(ctx context.Context, options ...rpc.Option) (*GetConfigurationReply, error) {
	res := &GetConfigurationReply{}
	err := c.requester.SendRequest(ctx, "platform.getConfiguration", struct{}{}, res, options...)
	return res, err
}

func (c *client) GetMultisigAlias(ctx context.Context, multisigAddress string, options ...rpc.Option) (*GetMultisigAliasReply, error) {
	res := &GetMultisigAliasReply{}
	err := c.requester.SendRequest(ctx, "platform.getMultisigAlias", &api.JSONAddress{
		Address: multisigAddress,
	}, res, options...)
	return res, err
}

func (c *client) GetAllDepositOffers(ctx context.Context, getAllDepositOffersArgs *GetAllDepositOffersArgs, options ...rpc.Option) (*GetAllDepositOffersReply, error) {
	res := &GetAllDepositOffersReply{}
	err := c.requester.SendRequest(ctx, "platform.getAllDepositOffers", &getAllDepositOffersArgs, res, options...)
	return res, err
}

func (c *client) GetRegisteredShortIDLink(ctx context.Context, addrStr ids.ShortID, options ...rpc.Option) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "platform.getMultisigAlias", &api.JSONAddress{
		Address: addrStr.String(),
	}, res, options...)
	return res.Address, err
}

func (c *client) GetLastAcceptedBlock(ctx context.Context, encoding formatting.Encoding, options ...rpc.Option) (any, error) {
	res := &api.GetBlockResponse{}
	err := c.requester.SendRequest(ctx, "platform.getLastAcceptedBlock", &api.Encoding{
		Encoding: encoding,
	}, res, options...)
	return res.Block, err
}

func (c *client) GetBlockAtHeight(ctx context.Context, height uint32, encoding formatting.Encoding, options ...rpc.Option) (any, error) {
	res := &api.GetBlockResponse{}
	err := c.requester.SendRequest(ctx, "platform.getBlockAtHeight", &GetBlockAtHeightArgs{
		Height:   height,
		Encoding: encoding,
	}, res, options...)
	return res.Block, err
}

func (c *client) GetClaimables(ctx context.Context, owners []*secp256k1fx.OutputOwners, options ...rpc.Option) ([]*state.Claimable, error) {
	res := &GetClaimablesReply{}
	if err := c.requester.SendRequest(ctx, "platform.getClaimables", &GetClaimablesArgs{
		Owners: apiOwnersFromSECP(owners),
	}, res, options...); err != nil {
		return nil, err
	}
	return claimablesFromAPI(res.Claimables)
}

func claimablesFromAPI(apiClaimables []APIClaimable) ([]*state.Claimable, error) {
	claimables := make([]*state.Claimable, len(apiClaimables))
	for i := range claimables {
		claimable, err := claimableFromAPI(&apiClaimables[i])
		if err != nil {
			return nil, err
		}
		claimables[i] = &claimable
	}
	return claimables, nil
}

func claimableFromAPI(apiClaimable *APIClaimable) (state.Claimable, error) {
	claimableOwner, err := secpOwnerFromAPI(&apiClaimable.RewardOwner)
	if err != nil {
		return state.Claimable{}, err
	}
	return state.Claimable{
		Owner:                claimableOwner,
		ValidatorReward:      uint64(apiClaimable.ValidatorRewards),
		ExpiredDepositReward: uint64(apiClaimable.ExpiredDepositRewards),
	}, nil
}

func apiOwnersFromSECP(secpOwners []*secp256k1fx.OutputOwners) []platformapi.Owner {
	owners := make([]platformapi.Owner, len(secpOwners))
	for i := range owners {
		owners[i] = *apiOwnerFromSECP(secpOwners[i])
	}
	return owners
}

func apiOwnerFromSECP(secpOwner *secp256k1fx.OutputOwners) *platformapi.Owner {
	apiOwner := &platformapi.Owner{
		Locktime:  json.Uint64(secpOwner.Locktime),
		Threshold: json.Uint32(secpOwner.Threshold),
		Addresses: make([]string, len(secpOwner.Addrs)),
	}
	for i := range secpOwner.Addrs {
		apiOwner.Addresses[i] = secpOwner.Addrs[i].String()
	}
	return apiOwner
}

func secpOwnerFromAPI(apiOwner *platformapi.Owner) (*secp256k1fx.OutputOwners, error) {
	if len(apiOwner.Addresses) > 0 {
		secpOwner := &secp256k1fx.OutputOwners{
			Locktime:  uint64(apiOwner.Locktime),
			Threshold: uint32(apiOwner.Threshold),
			Addrs:     make([]ids.ShortID, len(apiOwner.Addresses)),
		}
		for i := range apiOwner.Addresses {
			addr, err := parseAddr(apiOwner.Addresses[i])
			if err != nil {
				return nil, err
			}
			secpOwner.Addrs[i] = addr
		}
		secpOwner.Sort()
		return secpOwner, nil
	}
	return nil, nil
}

func parseAddr(addrStr string) (ids.ShortID, error) {
	addr, err1 := address.ParseToID(addrStr)
	if err1 == nil {
		return addr, nil
	}
	addr, err2 := ids.ShortFromString(addrStr)
	if err2 != nil {
		return ids.ShortEmpty, fmt.Errorf("failed to parse addr both as shortID (%s) and as bech32 (%s)",
			err2, err1)
	}
	return addr, nil
}
