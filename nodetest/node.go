// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/logging"

	nodeconfig "github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/utils/constants"
	"encoding/base64"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"encoding/json"
	"github.com/ava-labs/avalanchego/utils/units"
	"os"
)

func init() {
	evm.RegisterAllLibEVMExtras()
}

func setFlagDefaults(v *viper.Viper, dir string) {
	// TODO this can conflict with other local networks
	v.Set("network-id", "testing")
	v.Set("http-port", "0")
	v.Set("staking-port", "0")
	v.Set("data-dir", dir)
	// TODO register nodes as validators
	v.Set("sybil-protection-enabled", "false")
	v.Set("log-level", "debug")
	v.Set("log-display-level", "off")
	v.Set("log-disable-display-plugin-logs", true)
	v.Set("sybil-protection-enabled", false)
}

func newConfig(t *testing.T, v *viper.Viper) nodeconfig.Config {
	cfg, err := config.GetNodeConfig(v)
	if err != nil {
		require.NoError(t, err)
	}

	return cfg
}

type Config struct {
	BootstrapperIPs []netip.AddrPort
	BootstrapperIDs   []ids.NodeID
	GenesisFundedKeys []*secp256k1.PrivateKey
}

type Node struct {
	t *testing.T
	n *node.Node
}

// TODO shutdown
func Start(ctx context.Context, t *testing.T, cfg Config) *Node {
	fs := config.BuildFlagSet()

	// TODO this should not respect env vars
	v, err := config.BuildViper(fs, nil)
	require.NoError(t, err)

	dir, err := os.MkdirTemp("", "nodetest-*")
	require.NoError(t, err)

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("dir for debugging failure: %s", dir)
			return
		}

		_ = os.RemoveAll(dir)
	})

	setFlagDefaults(v, dir)

	// TODO default prefunded keys
	if len(cfg.GenesisFundedKeys) > 0 {
	}

	g := genesis.LocalConfig
	g.NetworkID = constants.UnitTestID
	// g.Allocations = []genesis.Allocation{}
	// g.InitialStakedFunds = []ids.ShortID{}
	// g.InitialStakers = []genesis.Staker{}

	for _, k := range cfg.GenesisFundedKeys {
		g.Allocations = append(g.Allocations, genesis.Allocation{
			AVAXAddr:      k.Address(),
			InitialAmount: 1_000_000 * units.Avax,
			UnlockSchedule: []genesis.LockedAmount{
				{
					Amount:   1_000_000 * units.Avax,
					Locktime: 0,
				},
			},
		})

		// g.InitialStakedFunds = append(g.InitialStakedFunds, k.Address())
		// g.InitialStakers = []genesis.Staker{}
	}

	require.NoError(t, err)

	unparsed, err := g.Unparse()
	require.NoError(t, err)

	b, err := json.Marshal(unparsed)
	require.NoError(t, err)

	v.Set("genesis-file-content", base64.StdEncoding.EncodeToString(b))

	v.Set("bootstrap-ips", strings.Join(stringify(cfg.BootstrapperIPs...), ","))
	v.Set("bootstrap-ids", strings.Join(stringify(cfg.BootstrapperIDs...), ","))

	nodeConfig := newConfig(t, v)

	// TODO close logs
	logFactory := logging.NewFactory(nodeConfig.LoggingConfig)
	// TODO log filtering
	log, err := logFactory.Make("main")
	require.NoError(t, err)

	inner, err := node.New(&nodeConfig, logFactory, log)
	require.NoError(t, err)

	eg, _ := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return inner.Dispatch()
	})

	n := &Node{t: t, n: inner}
	t.Logf("initialized node %s {http: %s, dir: %s}", n.ID(), n.HTTPAddress(), dir)

	return n
}

func stringify[T fmt.Stringer](ts ...T) []string {
	s := make([]string, 0, len(ts))

	for _, t := range ts {
		s = append(s, t.String())
	}

	return s
}

func (n *Node) StakingAddress() netip.AddrPort {
	return n.n.StakingAddress()
}

func (n *Node) HTTPAddress() netip.AddrPort {
	return n.n.HTTPAddress()
}

func (n *Node) ID() ids.NodeID {
	return n.n.ID
}

// TDOO cleanup
func newHealthClient(n *Node) *health.Client {
	return health.NewClient(fmt.Sprintf("http://%s", n.HTTPAddress()))
}

func awaitReady(t *testing.T, n *Node) {
	hc := newHealthClient(n)

	_, err := health.AwaitReady(t.Context(), hc, 50*time.Millisecond, []string{})
	require.NoError(t, err)
}

func awaitHealthy(t *testing.T, n *Node) {
	hc := newHealthClient(n)

	_, err := health.AwaitHealthy(t.Context(), hc, 50*time.Millisecond,
		[]string{})
	require.NoError(t, err)
}

type Block struct {
	Bytes  []byte
	Height uint64
}

func Accept(
	t *testing.T,
	chainID ids.ID,
	b Block,
	n *Node,
) {
	mc, err := message.NewCreator(
		prometheus.NewRegistry(),
		compression.TypeNone,
		time.Minute,
	)
	require.NoError(t, err)

	outMsg, err := mc.PushQuery(
		chainID,
		0,
		time.Hour,
		b.Bytes,
		b.Height-1,
	)
	require.NoError(t, err)

	outMsg.Bytes()

	inMsg, err := mc.Parse(outMsg.Bytes(), ids.GenerateTestNodeID(), func() {})
	require.NoError(t, err)

	n.n.Handle(t.Context(), inMsg)
}
