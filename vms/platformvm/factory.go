// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"crypto"
	"crypto/tls"
	"sync"

	coreth "github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	noderpcchainvm "github.com/ava-labs/avalanchego/node/rpcchainvm"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
)

var _ vms.Factory[*VM] = (*Factory)(nil)

func NewFactory(
	config config.Config,
	aliaser ids.Aliaser,
	uptimeLockedCalculator uptime.LockedCalculator,
	tlsCertificate tls.Certificate,
	stakingBLSKey *bls.SecretKey,
	tracer trace.Tracer,
	logFactory logging.Factory,
	vmManager noderpcchainvm.Manager,
	blockAcceptorGroup snow.AcceptorGroup,
	txAcceptorGroup snow.AcceptorGroup,
	vertexAcceptorGroup snow.AcceptorGroup,
	db database.Database,
	msgCreator message.OutboundMsgBuilder,
	router router.Router,
	net network.Network,
	bootstrappers validators.Manager,
	validators validators.Manager,
	nodeID ids.NodeID,
	keystore keystore.Keystore,
	atomicMemory *atomic.Memory,
	xChainID ids.ID,
	cChainID ids.ID,
	criticalChains set.Set[ids.ID],
	timeoutManager timeout.Manager,
	health health.Registerer,
	subnetConfigs map[ids.ID]subnets.Config,
	chainConfigs map[string]ChainConfig,
	shutdownNodeFunc func(exitCode int),
	metrics metrics.MultiGatherer,
	resourceTracker tracker.ResourceTracker,
) *Factory {
	return &Factory{
		config:                 config,
		aliaser:                aliaser,
		uptimeLockedCalculator: uptimeLockedCalculator,
		tlsCertificate:         tlsCertificate,
		stakingBLSKey:          stakingBLSKey,
		tracer:                 tracer,
		logFactory:             logFactory,
		avmFactory:             &avm.Factory{},
		corethFactory:          &coreth.Factory{},
		vmManager:              vmManager,
		blockAcceptorGroup:     blockAcceptorGroup,
		txAcceptorGroup:        txAcceptorGroup,
		vertexAcceptorGroup:    vertexAcceptorGroup,
		db:                     db,
		msgCreator:             msgCreator,
		router:                 router,
		net:                    net,
		bootstrappers:          bootstrappers,
		validators:             validators,
		nodeID:                 nodeID,
		keystore:               keystore,
		atomicMemory:           atomicMemory,
		xChainID:               xChainID,
		cChainID:               cChainID,
		criticalChains:         criticalChains,
		timeoutManager:         timeoutManager,
		health:                 health,
		subnetConfigs:          subnetConfigs,
		chainConfigs:           chainConfigs,
		shutdownNodeFunc:       shutdownNodeFunc,
		metrics:                metrics,
		resourceTracker:        resourceTracker,
	}
}

type Factory struct {
	config                 config.Config
	genesis                []byte
	aliaser                ids.Aliaser
	uptimeLockedCalculator uptime.LockedCalculator
	tlsCertificate         tls.Certificate
	stakingBLSKey          *bls.SecretKey
	tracer                 trace.Tracer
	logFactory             logging.Factory
	avmFactory             vms.Factory[*avm.VM]
	corethFactory          vms.Factory[*coreth.VM]
	vmManager              noderpcchainvm.Manager
	blockAcceptorGroup     snow.AcceptorGroup
	txAcceptorGroup        snow.AcceptorGroup
	vertexAcceptorGroup    snow.AcceptorGroup
	db                     database.Database
	msgCreator             message.OutboundMsgBuilder
	router                 router.Router
	net                    network.Network
	bootstrappers          validators.Manager
	validators             validators.Manager
	nodeID                 ids.NodeID
	keystore               keystore.Keystore
	atomicMemory           *atomic.Memory
	xChainID               ids.ID
	cChainID               ids.ID
	criticalChains         set.Set[ids.ID]
	timeoutManager         timeout.Manager
	health                 health.Registerer
	subnetConfigs          map[ids.ID]subnets.Config
	chainConfigs           map[string]ChainConfig
	shutdownNodeFunc       func(exitCode int)
	metrics                metrics.MultiGatherer
	resourceTracker        tracker.ResourceTracker
}

// New returns a new instance of the PlatformVM
func (f *Factory) New(log logging.Logger) (*VM, error) {
	vm := &VM{
		Config:                 f.config,
		aliaser:                f.aliaser,
		Validators:             f.validators,
		UptimeLockedCalculator: f.uptimeLockedCalculator,
	}

	vm.chainManager = &chainManager{
		Config: f.config,
		platformVMFactory: &factory{
			vm: vm,
		},
		avmFactory:             f.avmFactory,
		corethFactory:          f.corethFactory,
		aliaser:                f.aliaser,
		stakingBLSKey:          f.stakingBLSKey,
		tracer:                 f.tracer,
		log:                    log,
		logFactory:             f.logFactory,
		vmManager:              f.vmManager,
		blockAcceptorGroup:     f.blockAcceptorGroup,
		txAcceptorGroup:        f.txAcceptorGroup,
		vertexAcceptorGroup:    f.vertexAcceptorGroup,
		db:                     f.db,
		msgCreator:             f.msgCreator,
		router:                 f.router,
		net:                    f.net,
		bootstrappers:          f.bootstrappers,
		validators:             f.validators,
		nodeID:                 f.nodeID,
		keystore:               f.keystore,
		atomicMemory:           f.atomicMemory,
		xChainID:               f.xChainID,
		cChainID:               f.cChainID,
		criticalChains:         f.criticalChains,
		timeoutManager:         f.timeoutManager,
		health:                 f.health,
		subnetConfigs:          f.subnetConfigs,
		chainConfigs:           f.chainConfigs,
		shutdownNodeFunc:       f.shutdownNodeFunc,
		metrics:                f.metrics,
		resourceTracker:        f.resourceTracker,
		stakingSigner:          f.tlsCertificate.PrivateKey.(crypto.Signer),
		stakingCert:            staking.CertificateFromX509(f.tlsCertificate.Leaf),
		registrants:            nil,
		chainsQueue:            buffer.NewUnboundedBlockingDeque[chainParameters](initialQueueSize),
		unblockChainCreatorCh:  make(chan struct{}),
		chainCreatorShutdownCh: make(chan struct{}),
		chainCreatorExited:     sync.WaitGroup{},
		subnetsLock:            sync.RWMutex{},
		subnets:                make(map[ids.ID]subnets.Subnet),
		chainsLock:             sync.Mutex{},
		chains:                 make(map[ids.ID]handler.Handler),
		validatorState:         nil,
	}

	return vm.chainManager.StartChainCreator(chainParameters{
		ID:          constants.PlatformChainID,
		SubnetID:    constants.PrimaryNetworkID,
		GenesisData: f.genesis,
		VMID:        constants.PlatformVMID,
	})
}

type factory struct {
	vm *VM
}

func (f *factory) New(logging.Logger) (*VM, error) {
	return f.vm, nil
}
