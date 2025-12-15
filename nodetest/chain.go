// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

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

func CreateChain[T block.ChainVM](
	t *testing.T,
	n *Node,
	id ids.ID,
	factory vms.Factory,
	owner *secp256k1.PrivateKey,
	genesis []byte,
) (ids.ID, ids.ID, T) {
	// TODO make vms.Factory generic
	wrapper := &vmFactory[T]{factory: factory}
	require.NoError(t, n.n.VMManager.RegisterFactory(t.Context(), id, wrapper))

	awaitReady(t, n)

	w, err := primary.MakePWallet(
		t.Context(),
		fmt.Sprintf("http://%s", n.HTTPAddress()),
		secp256k1fx.NewKeychain(owner),
		primary.WalletConfig{},
	)
	require.NoError(t, err)

	createSubnetTx, err := w.IssueCreateSubnetTx(&secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{owner.Address()},
	})
	require.NoError(t, err)
	t.Logf("%s: created subnet %s", n.ID(), createSubnetTx.ID())

	createChainTx, err := w.IssueCreateChainTx(
		createSubnetTx.ID(),
		genesis,
		id,
		nil,
		id.String(),
	)
	require.NoError(t, err)
	t.Logf("%s: created chain %s", n.ID(), createChainTx.ID())

	awaitHealthy(t, n)

	return createSubnetTx.ID(), createChainTx.ID(), wrapper.vm
}
