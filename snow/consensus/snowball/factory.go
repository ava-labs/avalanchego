// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "github.com/ava-labs/avalanchego/ids"

var (
	SnowballFactory  Factory = snowballFactory{}
	SnowflakeFactory Factory = snowflakeFactory{}
)

type snowballFactory struct{}

func (snowballFactory) NewNnary(params Parameters, choice ids.ID) Nnary {
	sb := newNnarySnowball(params.BetaVirtuous, params.BetaRogue, choice)
	return &sb
}

func (snowballFactory) NewUnary(params Parameters) Unary {
	sb := newUnarySnowball(params.BetaVirtuous)
	return &sb
}

type snowflakeFactory struct{}

func (snowflakeFactory) NewNnary(params Parameters, choice ids.ID) Nnary {
	sf := newNnarySnowflake(params.BetaVirtuous, params.BetaRogue, choice)
	return &sf
}

func (snowflakeFactory) NewUnary(params Parameters) Unary {
	sf := newUnarySnowflake(params.BetaVirtuous)
	return &sf
}
