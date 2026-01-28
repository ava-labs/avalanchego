// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"

	nodeconfig "github.com/ava-labs/avalanchego/config/node"
	avalanchenode "github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/message"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/ava-labs/avalanchego/utils/compression"
)

func init() {
	evm.RegisterAllLibEVMExtras()
}

func setFlagDefaults(v *viper.Viper, dir string) {
	// TODO this can conflict with other local networks
	v.Set("network-id", "testing")
	v.Set("public-ip", "127.0.0.1")
	v.Set("http-port", "0")
	v.Set("data-dir", dir)
	v.Set("log-level", "info")
	v.Set("log-display-level", "off")
	v.Set("log-disable-display-plugin-logs", true)
	// Lower for faster test startup times
	v.Set("health-check-frequency", 2*time.Second)
	// Disable sybil protection so that we can validate any subnet the test
	// creates without knowing it in advance
	v.Set("sybil-protection-enabled", false)
}

func newConfig(t *testing.T, v *viper.Viper) nodeconfig.Config {
	cfg, err := config.GetNodeConfig(v)
	if err != nil {
		require.NoError(t, err)
	}

	return cfg
}

type nodeConfig struct {
	BootstrapperIPs   []netip.AddrPort
	BootstrapperIDs   []ids.NodeID
	GenesisFundedKeys []*secp256k1.PrivateKey
	GenesisValidators []genesis.Staker
	StakingPort       uint16
	StakingTLSCert    []byte
	StakingTLSCertKey []byte
	StakingSignerKey  *localsigner.LocalSigner
}

type Node struct {
	t *testing.T

	// Value set in [context.Context] to for debugging purposes
	id int
	// TODO make the upstream Node context aware so that we can wire add
	//  debugging context for conditional breakpoints
	n   *avalanchenode.Node
	dir string
}

func newNode(t *testing.T, cfg nodeConfig, id int) *Node {
	fs := config.BuildFlagSet()

	// TODO this should not respect env vars
	v, err := config.BuildViper(fs, nil)
	require.NoError(t, err)

	dir, err := os.MkdirTemp("", "nodetest-*")
	require.NoError(t, err)

	setFlagDefaults(v, dir)

	g := genesis.LocalConfig
	g.StartTime = uint64(time.Now().Unix())
	g.NetworkID = constants.UnitTestID
	g.InitialStakers = cfg.GenesisValidators

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
	}

	require.NoError(t, err)

	unparsed, err := g.Unparse()
	require.NoError(t, err)

	b, err := json.Marshal(unparsed)
	require.NoError(t, err)

	v.Set("genesis-file-content", base64.StdEncoding.EncodeToString(b))

	v.Set("bootstrap-ips", strings.Join(stringify(cfg.BootstrapperIPs...), ","))
	v.Set("bootstrap-ids", strings.Join(stringify(cfg.BootstrapperIDs...), ","))

	v.Set("staking-port", cfg.StakingPort)

	if cfg.StakingTLSCert != nil {
		v.Set(
			"staking-tls-cert-file-content",
			base64.StdEncoding.EncodeToString(cfg.StakingTLSCert),
		)
	}

	if cfg.StakingTLSCertKey != nil {
		v.Set(
			"staking-tls-key-file-content",
			base64.StdEncoding.EncodeToString(cfg.StakingTLSCertKey),
		)
	}

	if cfg.StakingSignerKey != nil {
		v.Set(
			"staking-signer-key-file-content",
			base64.StdEncoding.EncodeToString(cfg.StakingSignerKey.ToBytes()),
		)
	}

	nodeConfig := newConfig(t, v)

	// TODO close logs
	logFactory := logging.NewFactory(nodeConfig.LoggingConfig)
	// TODO log filtering
	log, err := logFactory.Make("main")
	require.NoError(t, err)

	n, err := avalanchenode.New(&nodeConfig, logFactory, log)
	require.NoError(t, err)

	return &Node{
		t:   t,
		id: id,
		n:   n,
		dir: dir,
	}
}

func stringify[T fmt.Stringer](ts ...T) []string {
	s := make([]string, 0, len(ts))

	for _, t := range ts {
		s = append(s, t.String())
	}

	return s
}

// Run returns after the node hits an error or the provided context is canceled.
func (n *Node) Run(ctx context.Context) error {
	n.t.Log("starting node:", n.ID(), n.HTTPAddress(), n.dir)

	done := make(chan error)
	go func() {
		done <- n.n.Dispatch()
	}()

	select {
	case <-ctx.Done():
		n.n.Shutdown(0)
		return nil
	case err := <-done:
		if err != nil {
			n.t.Log("error during node shutdown", n.ID(), err)
		}

		return err
	}
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

// TODO avoid rpc call?
func newHealthClient(n *Node) *health.Client {
	return health.NewClient(fmt.Sprintf("http://%s", n.HTTPAddress()))
}

func awaitReady(t *testing.T, n *Node, tags ...string) {
	hc := newHealthClient(n)

	_, err := health.AwaitReady(t.Context(), hc, time.Second, tags)
	require.NoError(t, err)
}

func awaitHealthy(t *testing.T, n *Node, tags ...string) {
	hc := newHealthClient(n)

	_, err := health.AwaitHealthy(t.Context(), hc, time.Second, tags)
	require.NoError(t, err)
}

func (n *Node) Accept(chainID ids.ID, b Block) {
	mc, err := message.NewCreator(
		prometheus.NewRegistry(),
		compression.TypeNone,
		time.Minute,
	)
	require.NoError(n.t, err)

	outMsg, err := mc.PushQuery(
		chainID,
		0,
		time.Hour,
		b.Bytes,
		b.Height-1,
	)
	require.NoError(n.t, err)

	// It does not matter who this message is from because we skip the
	// networking layer
	inMsg, err := mc.Parse(outMsg.Bytes, ids.GenerateTestNodeID(), func() {})
	require.NoError(n.t, err)

	n.n.HandleMessage(context.WithValue(n.t.Context(), DebugKey, n.id), inMsg)
}

type Block struct {
	Bytes  []byte
	Height uint64
}

type Validator[T block.ChainVM] struct {
	*Node
	VM T
}

func AwaitAcceptance[T block.ChainVM](
	t *testing.T,
	n *Validator[T],
	blkID ids.ID,
	height uint64,
) {
	await(func() bool {
		tip, err := n.VM.LastAccepted(t.Context())
		require.NoError(t, err)

		if tip == blkID {
			gotTip, err := n.VM.GetBlockIDAtHeight(t.Context(), height)
			require.NoError(t, err)

			require.Equal(t, tip, gotTip)
			return true
		}

		return false
	})
}

func await(doneF func() bool) {
	for {
		if doneF() {
			return
		}

		time.Sleep(time.Second)
	}
}

// func p2p helpers for apprequest and appgossip
