// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// ethService serves eth_baseFee in the same format as a node.
type ethService struct{}

func (ethService) BaseFee() *hexutil.Big {
	return (*hexutil.Big)(big.NewInt(25 * params.GWei))
}

// TestBaseFee checks the node's base fee is used only when no option sets one.
// The e2e tests always set one.
func TestBaseFee(t *testing.T) {
	server := rpc.NewServer()
	t.Cleanup(server.Stop)
	require.NoError(t, server.RegisterName("eth", ethService{}), "RegisterName()")
	client := rpc.DialInProc(server)
	t.Cleanup(client.Close)
	w := &wallet{ethClient: ethclient.NewClient(client)}

	tests := []struct {
		name    string
		options []common.Option
		want    gas.Price
	}{
		{
			name: "from_node",
			want: 25 * params.GWei,
		},
		{
			name:    "from_option",
			options: []common.Option{common.WithBaseFee(0)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := w.baseFee(test.options)
			require.NoError(t, err, "baseFee()")
			require.Equal(t, test.want, got, "baseFee()")
		})
	}
}
