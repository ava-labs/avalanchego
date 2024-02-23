// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"crypto"
	"crypto/tls"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

type FactoryConfig struct {
	NetworkID                               uint32
	PartialSyncPrimaryNetwork               bool
	SybilProtectionEnabled                  bool
	TracingEnabled                          bool
	MeterVMEnabled                          bool
	StateSyncBeacons                        []ids.NodeID
	BootstrapMaxTimeGetAncestors            time.Duration
	BootstrapAncestorsMaxContainersSent     int
	BootstrapAncestorsMaxContainersReceived int
	FrontierPollFrequency                   time.Duration
	ConsensusAppConcurrency                 int
	ApricotPhase4Time                       time.Time
	ApricotPhase4MinPChainHeight            uint64
	ChainDataDir                            string
	AvaxAssetID                             ids.ID
	SubnetConfigs                           map[ids.ID]subnets.Config
}

// Factory creates chains that are run by a node
type Factory struct {
	config FactoryConfig

	aliaser               ids.Aliaser
	stakingCert           *staking.Certificate
	stakingBLSKey         *bls.SecretKey
	stakingSigner         crypto.Signer
	tracer                trace.Tracer
	log                   logging.Logger
	logFactory            logging.Factory
	vmManager             vms.Manager
	blockAcceptorGroup    snow.AcceptorGroup
	txAcceptorGroup       snow.AcceptorGroup
	vertexAcceptorGroup   snow.AcceptorGroup
	db                    database.Database
	msgCreator            message.Creator
	router                router.Router
	net                   sender.ExternalSender
	validators            validators.Manager
	nodeID                ids.NodeID
	keystore              keystore.Keystore
	atomicMemory          *atomic.Memory
	xChainID              ids.ID
	cChainID              ids.ID
	timeoutManager        timeout.Manager
	health                health.Health
	chainConfigs          map[string]ChainConfig
	metrics               metrics.MultiGatherer
	resourceTracker       tracker.ResourceTracker
	subnets               *Subnets
	unblockChainCreatorCh chan struct{}
}

// NewFactory returns an instance of Factory
func NewFactory(
	config FactoryConfig,
	aliaser ids.Aliaser,
	stakingTLSCert tls.Certificate,
	stakingBLSKey *bls.SecretKey,
	tracer trace.Tracer,
	log logging.Logger,
	logFactory logging.Factory,
	vmManager vms.Manager,
	blockAcceptorGroup snow.AcceptorGroup,
	txAcceptorGroup snow.AcceptorGroup,
	vertexAcceptorGroup snow.AcceptorGroup,
	db database.Database,
	msgCreator message.Creator,
	router router.Router,
	net sender.ExternalSender,
	validators validators.Manager,
	nodeID ids.NodeID,
	keystore keystore.Keystore,
	atomicMemory *atomic.Memory,
	xChainID ids.ID,
	cChainID ids.ID,
	timeoutManager timeout.Manager,
	health health.Health,
	chainConfigs map[string]ChainConfig,
	metrics metrics.MultiGatherer,
	resourceTracker tracker.ResourceTracker,
	subnets *Subnets,
	unblockChainCreatorCh chan struct{},
) *Factory {
	return &Factory{
		config:                config,
		aliaser:               aliaser,
		stakingCert:           staking.CertificateFromX509(stakingTLSCert.Leaf),
		stakingBLSKey:         stakingBLSKey,
		stakingSigner:         stakingTLSCert.PrivateKey.(crypto.Signer),
		tracer:                tracer,
		log:                   log,
		logFactory:            logFactory,
		vmManager:             vmManager,
		blockAcceptorGroup:    blockAcceptorGroup,
		txAcceptorGroup:       txAcceptorGroup,
		vertexAcceptorGroup:   vertexAcceptorGroup,
		db:                    db,
		msgCreator:            msgCreator,
		router:                router,
		net:                   net,
		validators:            validators,
		nodeID:                nodeID,
		keystore:              keystore,
		atomicMemory:          atomicMemory,
		xChainID:              xChainID,
		cChainID:              cChainID,
		timeoutManager:        timeoutManager,
		health:                health,
		chainConfigs:          chainConfigs,
		metrics:               metrics,
		resourceTracker:       resourceTracker,
		subnets:               subnets,
		unblockChainCreatorCh: unblockChainCreatorCh,
	}
}
