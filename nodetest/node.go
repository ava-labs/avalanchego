// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"context"
	"github.com/ava-labs/avalanchego/config"
	nodeconfig "github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"testing"
	"net/netip"
	"strings"
	"fmt"
	"path/filepath"
)

func setFlagDefaults(v *viper.Viper, dir string) {
	v.Set("network-id", "local")
	v.Set("http-port", "0")
	v.Set("staking-port", "0")
	v.Set("data-dir", dir)
	// TODO register nodes as validators
	v.Set("sybil-protection-enabled", "false")
	v.Set("log-display-level", "off")
	v.Set("log-disable-display-plugin-logs", true)
}

func newConfig(t *testing.T, v *viper.Viper) nodeconfig.Config {
	cfg, err := config.GetNodeConfig(v)
	if err != nil {
		require.NoError(t, err)
	}

	return cfg
}

type Config struct {
	Name            string
	BootstrapperIPs []netip.AddrPort
	BootstrapperIDs []ids.NodeID
}

type Node struct {
	n *node.Node
}

func New(t *testing.T, c Config) Node {
	fs := config.BuildFlagSet()

	// TODO this should not respect env vars
	v, err := config.BuildViper(fs, nil)
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "main")
	t.Logf("initializing node at %s", dir)

	setFlagDefaults(v, dir)

	v.Set("bootstrap-ips", strings.Join(stringify(c.BootstrapperIPs...), ","))
	v.Set("bootstrap-ids", strings.Join(stringify(c.BootstrapperIDs...), ","))

	nodeConfig := newConfig(t, v)

	// TODO close logs
	logFactory := logging.NewFactory(nodeConfig.LoggingConfig)
	// TODO log filtering
	log, err := logFactory.Make("nodetest")
	require.NoError(t, err)

	n, err := node.New(&nodeConfig, logFactory, log)
	require.NoError(t, err)

	return Node{n: n}
}

func stringify[T fmt.Stringer](ts ...T) []string {
	s := make([]string, 0, len(ts))

	for _, t := range ts {
		s = append(s, t.String())
	}

	return s
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

func (n *Node) AddVM(
	ctx context.Context,
	id ids.ID,
	factory vms.Factory,
) error {
	return n.n.VMManager.RegisterFactory(ctx, id, factory)
}

func (n *Node) IP() netip.AddrPort {
	return n.n.GetIP()
}

func (n *Node) ID() ids.NodeID {
	return n.n.ID
}
