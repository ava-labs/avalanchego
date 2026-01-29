// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"

	avalanchegenesis "github.com/ava-labs/avalanchego/genesis"
)

// DebugKey is a key in the [context.Context] to set up conditional breakpoints
// Delve does not support conditional breakpoints on function calls currently
// so you will have to hack a breakpoint onto a line like so:
// _ = ctx.GetValue("nodetest-id")
const DebugKey = "nodetest-id"

type Subnet[T block.ChainVM] struct {
	SubnetID   ids.ID
	ChainID    ids.ID
	Validators []*Validator[T]
}

// NewSubnet creates a network of `n` validators running an instance of a chain
// specified by `factory`.
// TODO test on smaller networks
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

	validatorKeys := newValidatorKeys(t, n)
	cfg := nodeConfig{
		GenesisFundedKeys: fundedKeys,
	}

	validatorStakingKeys := make([]*secp256k1.PrivateKey, 0, n)

	for i, k := range validatorKeys {
		cfg.BootstrapperIPs = append(cfg.BootstrapperIPs, netip.AddrPortFrom(
			netip.AddrFrom4([4]byte{127, 0, 0, 1}),
			uint16(10_000+i),
		))

		nodeID := ids.NodeIDFromCert(k.stakingCert)
		cfg.BootstrapperIDs = append(cfg.BootstrapperIDs, nodeID)
		cfg.GenesisFundedKeys = append(cfg.GenesisFundedKeys, k.stakingKey)

		blsPK := bls.PublicKeyToCompressedBytes(k.blsKey.PublicKey())
		pop, err := k.blsKey.SignProofOfPossession(blsPK)
		require.NoError(t, err)

		cfg.GenesisValidators = append(cfg.GenesisValidators, avalanchegenesis.Staker{
			NodeID:        nodeID,
			RewardAddress: ids.ShortID{},
			DelegationFee: 0,
			Signer: &signer.ProofOfPossession{
				PublicKey:         [bls.PublicKeyLen]byte(blsPK),
				ProofOfPossession: [bls.SignatureLen]byte(bls.SignatureToBytes(pop)),
			},
		})

		validatorStakingKeys = append(validatorStakingKeys, k.stakingKey)
	}

	for i, k := range validatorKeys {
		eg.Go(func() error {
			nodeCfg := cfg
			nodeCfg.StakingTLSCert = k.stakingCertBytes
			nodeCfg.StakingTLSCertKey = k.stakingCertKeyBytes
			nodeCfg.StakingSignerKey = k.blsKey

			nodeCfg.StakingPort = uint16(10_000 + i)

			node := newNode(t, nodeCfg, i)
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
			t.Log("starting node", n.ID(), "with debug key", DebugKey, n.id)
			// Provide a context with a some debug context so we can set conditional
			// breakpoints
			nodeCtx := context.WithValue(egCtx, DebugKey, n.id)
			return n.Run(nodeCtx)
		})
	}

	subnetID, chainID := createChain(t, nodes, validatorStakingKeys, vmID, genesis)

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

// validatorKey is a set of keys used by a genesis validator
type validatorKey struct {
	stakingKey          *secp256k1.PrivateKey
	stakingCert         *staking.Certificate
	stakingCertBytes    []byte
	stakingCertKeyBytes []byte
	blsKey              *localsigner.LocalSigner
}

func newValidatorKeys(t *testing.T, n int) []*validatorKey {
	keys := make([]*validatorKey, 0, n)

	for range n {
		stakingKey, err := secp256k1.NewPrivateKey()
		require.NoError(t, err)

		certBytes, keyBytes, err := staking.NewCertAndKeyBytes()
		require.NoError(t, err)

		cert, err := tls.X509KeyPair(certBytes, keyBytes)
		require.NoError(t, err)

		cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)

		stakingCert, err := staking.ParseCertificate(cert.Leaf.Raw)
		require.NoError(t, err)

		blsKey, err := localsigner.New()
		require.NoError(t, err)

		keys = append(keys, &validatorKey{
			stakingKey:          stakingKey,
			stakingCert:         stakingCert,
			stakingCertBytes:    certBytes,
			stakingCertKeyBytes: keyBytes,
			blsKey:              blsKey,
		})
	}

	return keys
}

// createChain creates a chain and blocks until it is bootstrapped by all nodes
func createChain(
	t *testing.T,
	validators []*Node,
	validatorKeys []*secp256k1.PrivateKey,
	vmID ids.ID,
	genesis []byte,
) (ids.ID, ids.ID) {
	t.Log("waiting for node to be healthy:", validators[0].ID())
	awaitReady(t, validators[0])
	t.Log(validators[0].ID(), "reported healthy")

	w, err := primary.MakePWallet(
		t.Context(),
		fmt.Sprintf("http://%s", validators[0].HTTPAddress()),
		secp256k1fx.NewKeychain(validatorKeys[0]),
		primary.WalletConfig{},
	)
	require.NoError(t, err)

	addresses := make([]ids.ShortID, 0, len(validatorKeys))
	for _, k := range validatorKeys {
		addresses = append(addresses, k.Address())
	}

	t.Log("issuing create subnet tx to", validators[0].ID())
	createSubnetTx, err := w.IssueCreateSubnetTx(&secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     addresses,
	})
	require.NoError(t, err)
	t.Log(validators[0].ID(), "created subnet", createSubnetTx.ID())

	start := time.Now()
	end := start.Add(7 * 24 * time.Hour)

	for _, v := range validators {
		t.Log("adding validator", v.ID())

		// validator := txs.Validator{
		// 	NodeID: v.ID(),
		// 	Start:  uint64(start.Unix()),
		// 	End:    uint64(end.Unix()),
		// 	Wght:   2000 * units.Avax,
		// }
		//
		// 1234if _, err := w.IssueAddPermissionlessValidatorTx(
		// 	&txs.SubnetValidator{
		// 		Validator: validator,
		// 		Subnet:    constants.PrimaryNetworkID,
		// 	},
		// 	v.n.ProofOfPossession(),
		// 	v.n.Config.AvaxAssetID,
		// 	&secp256k1fx.OutputOwners{
		// 		Threshold: 1,
		// 		Addrs:     []ids.ShortID{{}},
		// 	},
		// 	&secp256k1fx.OutputOwners{
		// 		Threshold: 1,
		// 		Addrs:     []ids.ShortID{{}},
		// 	},
		// 	20_000, // 2% delegation fee
		// ); err != nil {
		// 	require.NoError(t, err)
		// }

		if _, err = w.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: v.ID(),
					Start:  uint64(start.Unix()),
					End:    uint64(end.Unix()),
					Wght:   1000,
				},
				Subnet: createSubnetTx.ID(),
			},
		); err != nil {
			require.NoError(t, err)
		}
	}

	// require.NoError(t, eg.Wait())
	t.Logf("issuing create chain tx to %s", validators[0].ID())
	createChainTx, err := w.IssueCreateChainTx(
		createSubnetTx.ID(),
		genesis,
		vmID,
		nil,
		vmID.String(),
	)
	require.NoError(t, err)
	t.Logf("created chain %s", createChainTx.ID())

	eg := &errgroup.Group{}
	for _, n := range validators {
		eg.Go(func() error {
			awaitHealthy(t, n, createChainTx.ID().String())
			return nil
		})
	}

	t.Log("waiting for subnet to bootstrap")
	require.NoError(t, eg.Wait())

	t.Log("finished bootstrapping subnet")
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
