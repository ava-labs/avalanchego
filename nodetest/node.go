// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"context"
	"github.com/ava-labs/avalanchego/config"
	nodeconfig "github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"path/filepath"
	"testing"
)

func setFlagDefaults(t *testing.T, v *viper.Viper) {
	v.Set("network-id", "local")
	v.Set("http-port", "0")
	v.Set("staking-port-port", "0")
	v.Set("data-dir", filepath.Join(t.TempDir(), "avalanchego.test"))
}

func newConfig(t *testing.T, v *viper.Viper) nodeconfig.Config {
	// TODO sybil protection enabled + run as a valdiator

	cfg, err := config.GetNodeConfig(v)
	if err != nil {
		require.NoError(t, err)
	}

	return cfg
}

type Node struct {
	n *node.Node
}

func New(t *testing.T) Node {
	fs := config.BuildFlagSet()

	// TODO this should not respect env vars
	v, err := config.BuildViper(fs, nil)
	require.NoError(t, err)

	setFlagDefaults(t, v)

	cfg := newConfig(t, v)

	// TODO close logs
	logFactory := logging.NewFactory(cfg.LoggingConfig)
	// TODO log filtering
	log, err := logFactory.Make("nodetest")
	require.NoError(t, err)

	n, err := node.New(&cfg, logFactory, log)
	require.NoError(t, err)

	return Node{n: n}
}

//	func NewWithBootstrapper(t *testing.T, bootstrapper Node) Node {
//		n := New(t)
//
// }

func (n *Node) Start(ctx context.Context) error {
	eg, _ := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return n.n.Dispatch()
	})

	// TODO call shutdown
	return eg.Wait()
}

func (n *Node) AddVM(vm block.ChainVM) {

}
