// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"context"
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/example/timestampvm"
	tsvm_client "github.com/ava-labs/avalanchego/vms/example/timestampvm/client"
)

var (
	tsvmSubnetName = "timestamp"

	genesisBytes = []byte("e2e")

	TSVMSubnet = &tmpnet.Subnet{
		Name: tsvmSubnetName,
		Chains: []*tmpnet.Chain{
			{
				VMName:  "timestamp",
				Genesis: genesisBytes,
				Config:  "{}",
			},
		},
	}
)

var _ = ginkgo.Describe("[TimestampVM]", ginkgo.Ordered, func() {
	require := require.New(ginkgo.GinkgoT())

	var (
		gid     ids.ID
		clients []tsvm_client.Client
	)

	ginkgo.BeforeAll(func() {
		network := e2e.Env.GetNetwork()
		subnet := network.GetSubnet(tsvmSubnetName)
		require.NotNil(subnet)
		chainID := subnet.Chains[0].ChainID

		clients = make([]tsvm_client.Client, len(e2e.Env.URIs))
		for i := range e2e.Env.URIs {
			chainURI := e2e.Env.URIs[i].URI + fmt.Sprintf("/ext/bc/%s", chainID)
			clients[i] = tsvm_client.New(chainURI)
		}
	})

	ginkgo.It("should support retrieval of the genesis block", func() {
		for _, client := range clients {
			timestamp, data, height, id, _, err := client.GetBlock(e2e.DefaultContext(), nil)
			require.NoError(err)
			require.Zero(timestamp)
			require.Equal(data, timestampvm.BytesToData(genesisBytes))
			require.Zero(height)
			gid = id
		}
	})

	data := timestampvm.BytesToData(hashing.ComputeHash256([]byte("test")))
	now := time.Now().Unix()
	ginkgo.It("should support creation of a new block", func() {
		success, err := clients[0].ProposeBlock(e2e.DefaultContext(), data)
		require.NoError(err)
		require.True(success)
	})

	ginkgo.It("should process new block on all nodes", func() {
		for i, client := range clients {
			ginkgo.By(fmt.Sprintf(" waiting for height to increase on node %d", i))
			e2e.Eventually(func() bool {
				timestamp, blockData, height, _, pid, err := client.GetBlock(context.Background(), nil)
				require.NoError(err)
				if height == 0 {
					return false
				}
				require.Greater(timestamp, uint64(now)-5)
				require.Equal(data, blockData)
				require.Equal(height, uint64(1))
				require.Equal(gid, pid)
				return true
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see height increase before timeout")
		}
	})
})
