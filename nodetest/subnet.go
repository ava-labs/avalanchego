// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms"
	"golang.org/x/sync/errgroup"
	"net/netip"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"fmt"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/utils/logging"
	"context"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"time"
)

type Subnet[T block.ChainVM] struct {
	SubnetID   ids.ID
	ChainID    ids.ID
	Validators []*Validator[T]
}

// NewSubnet creates a network of `n` validators running an instance of a chain
// specified by `factory`.
func NewSubnet[T block.ChainVM](
	t *testing.T,
	n int,
	factory vms.Factory,
	fundedKeys []*secp256k1.PrivateKey,
	genesis []byte,
) *Subnet[T] {
	if n < 2 {
		panic("subnets must have > 1 node")
	}

	nodes := make([]*Node, n)
	vms := make([]*vmFactory[T], n)
	vmID := ids.GenerateTestID()
	eg := &errgroup.Group{}

	bootstrapperCfg := nodeConfig{GenesisFundedKeys: fundedKeys}
	nodes[0] = newNode(t, bootstrapperCfg)
	vms[0] = &vmFactory[T]{factory: factory}
	require.NoError(t, nodes[0].n.VMManager.RegisterFactory(
		t.Context(),
		vmID,
		vms[0],
	))

	for i := 1; i < n; i++ {
		eg.Go(func() error {
			cfg := bootstrapperCfg
			cfg.BootstrapperIPs = []netip.AddrPort{nodes[0].StakingAddress()}
			cfg.BootstrapperIDs = []ids.NodeID{nodes[0].ID()}

			node := newNode(t, cfg)
			nodes[i] = node

			vms[i] = &vmFactory[T]{factory: factory}
			require.NoError(t, node.n.VMManager.RegisterFactory(
				t.Context(),
				vmID,
				vms[i],
			))

			return nil
		})
	}

	require.NoError(t, eg.Wait())

	var egCtx context.Context
	eg, egCtx = errgroup.WithContext(t.Context())

	for _, n := range nodes {
		eg.Go(func() error {
			return n.Run(egCtx)
		})
	}

	time.Sleep(time.Hour)
	subnetID, chainID := createChain(t, nodes, vmID, fundedKeys, genesis)

	validators := make([]*Validator[T], 0, n)
	for i, node := range nodes {
		validators = append(validators, &Validator[T]{
			Node: node,
			VM:   vms[i].vm,
		})
	}

	return &Subnet[T]{
		SubnetID:   subnetID,
		ChainID:    chainID,
		Validators: validators,
	}
}

// createChain creates a chain and blocks until it is bootstrapped by all nodes
func createChain(
	t *testing.T,
	nodes []*Node,
	vmID ids.ID,
	owners []*secp256k1.PrivateKey,
	genesis []byte,
) (ids.ID, ids.ID) {
	t.Log("waiting for node to be healthy:", nodes[0].ID())
	awaitReady(t, nodes[0])
	t.Log(nodes[0].ID(), "reported healthy")

	w, err := primary.MakePWallet(
		t.Context(),
		fmt.Sprintf("http://%s", nodes[0].HTTPAddress()),
		secp256k1fx.NewKeychain(owners...),
		primary.WalletConfig{},
	)
	require.NoError(t, err)

	addresses := make([]ids.ShortID, 0, len(owners))
	for _, o := range owners {
		addresses = append(addresses, o.Address())
	}

	t.Log("issuing create subnet tx to", nodes[0].ID())
	createSubnetTx, err := w.IssueCreateSubnetTx(&secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     addresses,
	})
	require.NoError(t, err)
	t.Log(nodes[0].ID(), "created subnet", createSubnetTx.ID())

	start := time.Now()
	end := start.Add(7 * 24 * time.Hour)

	for _, n := range nodes {
		_, err := w.IssueAddSubnetValidatorTx(&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: n.ID(),
				Start:  uint64(start.Unix()),
				End:    uint64(end.Unix()),
				Wght:   1,
			},
			Subnet: createSubnetTx.ID(),
		})
		require.NoError(t, err)
	}

	createChainTx, err := w.IssueCreateChainTx(
		createSubnetTx.ID(),
		genesis,
		vmID,
		nil,
		vmID.String(),
	)
	require.NoError(t, err)
	t.Logf("%s: created chain %s", nodes[0].ID(), createChainTx.ID())

	eg := errgroup.Group{}

	for _, n := range nodes {
		eg.Go(func() error {
			awaitHealthy(t, n, createChainTx.ID().String())
			return nil
		})
	}

	require.NoError(t, eg.Wait())

	return createSubnetTx.ID(), createChainTx.ID()
}

var _ vms.Factory = (*vmFactory[block.ChainVM])(nil)

type vmFactory[T block.ChainVM] struct {
	factory vms.Factory
	vm      T
}

func (v *vmFactory[T]) New(log logging.Logger) (any, error) {
	vm, err := v.factory.New(log)
	if err != nil {
		return nil, err
	}

	v.vm = vm.(T)

	return vm, nil
}
