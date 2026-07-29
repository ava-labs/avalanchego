// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"
)

func TestNewPriceOptions(t *testing.T) {
	minimumPrice := &Price{
		GasTip: (*hexutil.Big)(big.NewInt(params.Wei)),
		GasFee: (*hexutil.Big)(big.NewInt(2 * params.Wei)),
	}
	const (
		tip     = 500
		baseFee = 100
	)
	tests := []struct {
		name    string
		tip     uint64
		baseFee uint64
		want    *PriceOptions
	}{
		{
			name:    "minimum",
			tip:     params.Wei,
			baseFee: params.Wei,
			want: &PriceOptions{
				Slow:   minimumPrice,
				Normal: minimumPrice,
				Fast:   minimumPrice,
			},
		},
		{
			name:    "percentages",
			tip:     tip,
			baseFee: baseFee,
			want: &PriceOptions{
				Slow:   newPrice(big.NewInt(tip*slowTipPercent/100), big.NewInt(baseFee)),
				Normal: newPrice(big.NewInt(tip), big.NewInt(baseFee)),
				Fast:   newPrice(big.NewInt(tip*fastTipPercent/100), big.NewInt(baseFee)),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tip := new(big.Int).SetUint64(test.tip)
			baseFee := new(big.Int).SetUint64(test.baseFee)
			got := NewPriceOptions(tip, baseFee)
			require.Equalf(t, test.want, got, "NewPriceOptions(%s, %v)", tip, baseFee)
		})
	}
}

// TestCustomAPICarriesNoSubscription enforces the [customSubscriptionAPI]
// split.
func TestCustomAPICarriesNoSubscription(t *testing.T) {
	subscription := reflect.TypeFor[*rpc.Subscription]()

	api := reflect.TypeFor[*customAPI]()
	for i := range api.NumMethod() {
		m := api.Method(i)
		for j := range m.Type.NumOut() {
			require.NotEqualf(t, subscription, m.Type.Out(j),
				"%s.%s returns %s; declare it on customSubscriptionAPI instead",
				api, m.Name, subscription,
			)
		}
	}
}
