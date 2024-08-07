// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

type CaminoClient interface {
	// GetConfiguration returns genesis information of the primary network
	GetConfiguration(ctx context.Context, options ...rpc.Option) (*GetConfigurationReply, error)

	// GetMultisigAlias returns the alias definition of the given multisig address
	GetMultisigAlias(ctx context.Context, multisigAddress string, options ...rpc.Option) (*GetMultisigAliasReply, error)

	GetAllDepositOffers(ctx context.Context, getAllDepositOffersArgs *GetAllDepositOffersArgs, options ...rpc.Option) (*GetAllDepositOffersReply, error)
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
