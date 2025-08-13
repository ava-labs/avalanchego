// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "github.com/ava-labs/avalanchego/ids"

var (
	SnowballFactory  Factory = snowballFactory{}
	SnowflakeFactory Factory = snowflakeFactory{}
)

type snowballFactory struct{}

func (snowballFactory) NewNnary(params Parameters, choice ids.ID) Nnary {
	sb := newNnarySnowball(params.AlphaPreference, newSingleTerminationCondition(params.AlphaConfidence, params.Beta), choice)
	return &sb
}

func (snowballFactory) NewUnary(params Parameters) Unary {
	sb := newUnarySnowball(params.AlphaPreference, newSingleTerminationCondition(params.AlphaConfidence, params.Beta))
	return &sb
}

type snowflakeFactory struct{}

func (snowflakeFactory) NewNnary(params Parameters, choice ids.ID) Nnary {
	sf := newNnarySnowflake(params.AlphaPreference, newSingleTerminationCondition(params.AlphaConfidence, params.Beta), choice)
	return &sf
}

func (snowflakeFactory) NewUnary(params Parameters) Unary {
	sf := newUnarySnowflake(params.AlphaPreference, newSingleTerminationCondition(params.AlphaConfidence, params.Beta))
	return &sf
}
