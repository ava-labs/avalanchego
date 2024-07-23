// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

var (
	SnowballFactory  Factory = snowballFactory{}
	SnowflakeFactory Factory = snowflakeFactory{}
)

type snowballFactory struct{}

func (snowballFactory) NewUnary(params Parameters) Unary {
	sb := newUnarySnowball(params.AlphaPreference, newSingleTerminationCondition(params.AlphaConfidence, params.Beta))
	return &sb
}

type snowflakeFactory struct{}

func (snowflakeFactory) NewUnary(params Parameters) Unary {
	sf := newUnarySnowflake(params.AlphaPreference, newSingleTerminationCondition(params.AlphaConfidence, params.Beta))
	return &sf
}
