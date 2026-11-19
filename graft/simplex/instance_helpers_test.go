// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	metadata "github.com/ava-labs/simplex/msm"
	"github.com/ava-labs/simplex/testutil"
	"github.com/ava-labs/simplex/wal"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type testInnerBlock struct {
	Height_ uint64
	TS      time.Time
	Payload []byte
}

func (b *testInnerBlock) Bytes() []byte {
	out := make([]byte, 16, 16+len(b.Payload))
	binary.BigEndian.PutUint64(out[0:8], b.Height_)
	binary.BigEndian.PutUint64(out[8:16], uint64(b.TS.UnixMilli()))
	out = append(out, b.Payload...)
	return out
}

func (b *testInnerBlock) Digest() [32]byte {
	bytes := b.Bytes()
	return sha256.Sum256(bytes)
}

func (b *testInnerBlock) Height() uint64                       { return b.Height_ }
func (b *testInnerBlock) Timestamp() time.Time                 { return b.TS }
func (b *testInnerBlock) Verify(context.Context, uint64) error { return nil }

type testInnerBlockDeserializer struct{}

func (ibd *testInnerBlockDeserializer) ParseBlock(_ context.Context, buff []byte) (avalanchego.VMBlock, error) {
	b := &testInnerBlock{}
	b.Height_ = binary.BigEndian.Uint64(buff[0:8])
	b.TS = time.UnixMilli(int64(binary.BigEndian.Uint64(buff[8:16])))
	b.Payload = append([]byte(nil), buff[16:]...)
	return b, nil
}

var (
	genesisPChainHeight uint64 = 0
	genesisBlock               = &testInnerBlock{Height_: genesisPChainHeight, TS: time.Now(), Payload: []byte("genesis")}
)

// walGCInterval is the number of sequences after which instances under test garbage
// collect their WAL.
const walGCInterval = 10

var paramConfig = ParameterConfig{
	MaxNetworkDelay: 200 * time.Millisecond,
	MaxRoundWindow:  100,
	WALMaxSizeBytes: 1024,
	WALGCInterval:   walGCInterval,
}

// paramsWithMaxBlacklistedRounds returns the shared paramConfig with the given max blacklisted rounds.
func paramsWithMaxBlacklistedRounds(rounds uint16) *ParameterConfig {
	params := paramConfig
	params.MaxBlacklistedRounds = rounds
	return &params
}

type testPlatformChain struct {
	genesisHeight uint64 // genesis height is the height of the pchain the genesis validator set lives

	// lock guards the sets, which the running instances read while a test installs new ones.
	lock sync.Mutex
	// validatorSetAtHeight maps a P-chain height to the validator set in force from it on.
	validatorSetAtHeight map[uint64]metadata.NodeBLSMappings

	height uint64
	// heightChanged is closed and replaced on every advanceHeight, waking waiters
	// so they re-check the height.
	heightChanged chan struct{}
}

// newTestPChain returns a P-chain holding only the genesis validator set, in force from
// genesisPChainHeight on.
func newTestPChain(genesisSet metadata.NodeBLSMappings) *testPlatformChain {
	return &testPlatformChain{
		genesisHeight: genesisPChainHeight,
		validatorSetAtHeight: map[uint64]metadata.NodeBLSMappings{
			genesisPChainHeight: genesisSet,
		},
		height:        genesisPChainHeight,
		heightChanged: make(chan struct{}),
	}
}

func (pc *testPlatformChain) currentHeight() uint64 {
	pc.lock.Lock()
	defer pc.lock.Unlock()
	return pc.height
}

// GetValidatorSet grabs the validator set at `height`. If one does not directly exist at that height,
// it returns validator set at the first height less than `height`.
func (pc *testPlatformChain) GetValidatorSet(height uint64) (metadata.NodeBLSMappings, error) {
	pc.lock.Lock()
	defer pc.lock.Unlock()

	set, ok := pc.validatorSetAtHeight[height]
	if ok {
		return set, nil
	}

	var nextLargestHeight uint64
	for h := range pc.validatorSetAtHeight {
		if h >= height {
			continue
		}

		if h > nextLargestHeight {
			nextLargestHeight = h
		}
	}

	set, ok = pc.validatorSetAtHeight[nextLargestHeight]
	if !ok {
		return nil, fmt.Errorf("no validator set at %d", height)
	}

	return set, nil
}

func (pc *testPlatformChain) GenesisValidatorSet() metadata.NodeBLSMappings {
	pc.lock.Lock()
	defer pc.lock.Unlock()

	return pc.validatorSetAtHeight[pc.genesisHeight]
}

func (pc *testPlatformChain) GetMinimumHeight() uint64 {
	return pc.currentHeight()
}

func (pc *testPlatformChain) GetCurrentHeight() uint64 {
	return pc.currentHeight()
}

// WaitForProgress blocks until the context is cancelled or the P-chain height
// has increased past pChainHeight.
func (pc *testPlatformChain) WaitForProgress(ctx context.Context, pChainHeight uint64) error {
	for {
		pc.lock.Lock()
		if pc.height > pChainHeight {
			pc.lock.Unlock()
			return nil
		}
		ch := pc.heightChanged
		pc.lock.Unlock()

		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (pc *testPlatformChain) setValidatorSetAt(height uint64, validatorSet metadata.NodeBLSMappings) {
	pc.lock.Lock()
	defer pc.lock.Unlock()

	pc.validatorSetAtHeight[height] = validatorSet
}

// advanceHeight bumps the P-chain height and wakes every WaitForProgress waiter.
func (pc *testPlatformChain) advanceHeight(height uint64) {
	pc.lock.Lock()
	defer pc.lock.Unlock()

	if height <= pc.height {
		panic("smaller height")
	}
	pc.height = height
	close(pc.heightChanged)
	pc.heightChanged = make(chan struct{})
}

func (pc *testPlatformChain) LastNonSimplexBlockPChainHeight() uint64 {
	return pc.genesisHeight
}

type testCryptoOps struct{}

func (c *testCryptoOps) Sign(message []byte) (common.SignatureBytes, error) {
	d := sha256.Sum256(message)
	return d[:], nil
}

func (c *testCryptoOps) AggregateKeys(keys ...common.PublicKeyBytes) (common.PublicKeyBytes, error) {
	var out common.PublicKeyBytes
	for _, k := range keys {
		out = append(out, k...)
	}
	return out, nil
}

func (c *testCryptoOps) VerifySignature(message []byte, signature common.SignatureBytes, _ common.PublicKeyBytes) error {
	if len(signature) == 0 || len(signature)%sha256.Size != 0 {
		return fmt.Errorf("signature of %d bytes is not a whole number of digests", len(signature))
	}
	expected := sha256.Sum256(message)
	for i := 0; i < len(signature); i += sha256.Size {
		if !bytes.Equal(signature[i:i+sha256.Size], expected[:]) {
			return fmt.Errorf("signature does not match the message")
		}
	}
	return nil
}

func (c *testCryptoOps) CreateSignatureAggregator(nodes []common.Node) common.SignatureAggregator {
	return &testutil.TestSignatureAggregator{N: len(nodes)}
}

func (c *testCryptoOps) DeserializeQuorumCertificate(bytes []byte) (common.QuorumCertificate, error) {
	var qc []common.Signature
	if _, err := asn1.Unmarshal(bytes, &qc); err != nil {
		return nil, err
	}
	return testutil.TestQC(qc), nil
}

// testStorage serves reads as independent StateMachineBlock copies, so tests never touch
// the instance's live block objects, whose canoto caches the instance keeps mutating.
type testStorage struct {
	*testutil.InMemStorage
}

func newTestStorage() *testStorage {
	return &testStorage{InMemStorage: testutil.NewInMemStorage()}
}

func newTestStorageWithGenesis(t *testing.T) *testStorage {
	return newTestStorageWithPreSimplexBlocks(t, genesisBlock)
}

func newTestStorageWithPreSimplexBlocks(t *testing.T, blocks ...*testInnerBlock) *testStorage {
	s := newTestStorage()
	for _, inner := range blocks {
		block := &ParsedBlock{StateMachineBlock: metadata.StateMachineBlock{
			InnerBlock: inner,
			Metadata: metadata.StateMachineMetadata{
				SimplexProtocolMetadata: common.ProtocolMetadata{Seq: inner.Height()},
			},
		}}
		require.NoError(t, s.Index(context.Background(), block, common.Finalization{}))
	}
	return s
}

func (m *testStorage) GetBlock(seq uint64) (metadata.StateMachineBlock, *common.Finalization, error) {
	block, fin, err := m.Retrieve(seq)
	if err != nil {
		return metadata.StateMachineBlock{}, nil, err
	}
	parsed, ok := block.(*ParsedBlock)
	if !ok {
		return metadata.StateMachineBlock{}, nil, fmt.Errorf("expected *ParsedBlock at seq %d, got %T", seq, block)
	}
	return parsed.Clone(), &fin, nil
}

type walCreator struct {
	t *testing.T

	lock sync.Mutex
	wals []*testutil.TestWAL
}

func (w *walCreator) createWAL() (wal.DeletableWAL, error) {
	tw := testutil.NewTestWAL(w.t)
	w.lock.Lock()
	w.wals = append(w.wals, tw)
	w.lock.Unlock()
	return tw, nil
}

// containsNotarization reports whether any WAL this creator handed out holds a
// notarization for the given round.
func (w *walCreator) containsNotarization(round uint64) bool {
	w.lock.Lock()
	defer w.lock.Unlock()

	for _, tw := range w.wals {
		if tw.ContainsNotarization(round) {
			return true
		}
	}
	return false
}

// reparseBlock rebuilds an independent ParsedBlock from a verified block's wire bytes.
// The zero block has no inner block, so its inner bytes stay empty.
func reparseBlock(t *testing.T, vb common.VerifiedBlock) *ParsedBlock {
	var rawBlock metadata.RawBlock
	require.NoError(t, rawBlock.UnmarshalCanoto(vb.Bytes()))

	var inner avalanchego.VMBlock
	if len(rawBlock.InnerBlockBytes) > 0 {
		bd := &testInnerBlockDeserializer{}
		parsed, err := bd.ParseBlock(context.Background(), rawBlock.InnerBlockBytes)
		require.NoError(t, err)
		inner = parsed
	}
	return &ParsedBlock{
		StateMachineBlock: metadata.StateMachineBlock{InnerBlock: inner, Metadata: rawBlock.Metadata},
	}
}

func verifiedQuorumRoundToQuorumRound(t *testing.T, vqr common.VerifiedQuorumRound) common.QuorumRound {
	qr := common.QuorumRound{
		Notarization:      vqr.Notarization,
		Finalization:      vqr.Finalization,
		EmptyNotarization: vqr.EmptyNotarization,
	}
	if vqr.VerifiedBlock != nil {
		qr.Block = reparseBlock(t, vqr.VerifiedBlock)
	}
	return qr
}

type inflightMessage struct {
	to   *Instance
	from common.NodeID
	msg  *common.Message
}

type instanceComm struct {
	n *network
	// id is the node this comm belongs to, reported as the sender of every message it sends.
	id common.NodeID

	queue chan inflightMessage

	// closed is shut by stop, releasing run and any pending enqueue.
	closed    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

const maxInFlightMessages = 10000

func newInstanceComm(n *network, id common.NodeID) *instanceComm {
	return &instanceComm{
		n:      n,
		id:     id,
		queue:  make(chan inflightMessage, maxInFlightMessages),
		closed: make(chan struct{}),
	}
}

// start allows messages to be processed
func (i *instanceComm) start() {
	i.wg.Go(func() {
		i.run()
	})
}

// run sends queued messages one at a time until stop is called.
func (i *instanceComm) run() {
	for {
		select {
		case <-i.closed:
			return
		case m := <-i.queue:
			if err := m.to.HandleMessage(m.msg, m.from); err != nil {
				panic(err)
			}
		}
	}
}

// stop stops message sending and waits for the in-flight message to finish.
func (i *instanceComm) stop() {
	i.closeOnce.Do(func() { close(i.closed) })
	i.wg.Wait()
}

// enqueue adds a message to be sent
func (i *instanceComm) enqueue(m inflightMessage) {
	select {
	case i.queue <- m:
	default:
		i.n.t.Errorf("node %x dropped a message, queue is full at %d", i.id, maxInFlightMessages)
	}
}

func (c *instanceComm) Send(msg *common.Message, destination common.NodeID) {
	if c.n.isOffline(c.id) || c.n.isOffline(destination) {
		return
	}

	for _, n := range c.n.nodesSnapshot() {
		if !bytes.Equal(n.id, destination) {
			continue
		}

		require.NotNil(c.n.t, n.inst, "node %x was sent a message before it was created", destination)
		c.enqueue(inflightMessage{
			to:   n.inst,
			from: c.id,
			msg:  translateOutgoingToIncomingMessage(c.n.t, msg),
		})
		return
	}
}

func (c *instanceComm) Broadcast(msg *common.Message) {
	if c.n.isOffline(c.id) {
		return
	}

	// every node in the network but ourselves, each with its own re-parsed copy
	for _, n := range c.n.nodesSnapshot() {
		if bytes.Equal(n.id, c.id) || c.n.isOffline(n.id) {
			continue
		}

		require.NotNil(c.n.t, n.inst, "node %x was sent a message before it was created", n.id)
		c.enqueue(inflightMessage{
			to:   n.inst,
			from: c.id,
			msg:  translateOutgoingToIncomingMessage(c.n.t, msg),
		})
	}
}

// translateOutgoingToIncomingMessage converts the verified message types an instance
// sends into the wire types a receiver handles, like testutil.TestComm. Each carried
// block is re-parsed into a fresh ParsedBlock because HandleMessage mutates the block
// it receives, so recipients cannot share the sender's live block.
func translateOutgoingToIncomingMessage(t *testing.T, msg *common.Message) *common.Message {
	switch {
	case msg.VerifiedBlockMessage != nil:
		return &common.Message{
			BlockMessage: &common.BlockMessage{
				Vote:  msg.VerifiedBlockMessage.Vote,
				Block: reparseBlock(t, msg.VerifiedBlockMessage.VerifiedBlock),
			},
		}
	case msg.VerifiedReplicationResponse != nil:
		vrr := msg.VerifiedReplicationResponse
		data := make([]common.QuorumRound, 0, len(vrr.Data))
		for _, vqr := range vrr.Data {
			data = append(data, verifiedQuorumRoundToQuorumRound(t, vqr))
		}
		resp := &common.ReplicationResponse{Data: data}
		if vrr.LatestRound != nil {
			qr := verifiedQuorumRoundToQuorumRound(t, *vrr.LatestRound)
			resp.LatestRound = &qr
		}
		if vrr.LatestFinalizedSeq != nil {
			qr := verifiedQuorumRoundToQuorumRound(t, *vrr.LatestFinalizedSeq)
			resp.LatestSeq = &qr
		}
		return &common.Message{ReplicationResponse: resp}
	default:
		return msg
	}
}

// pendingBlockSignal is the network's shared mempool. It stays pending until a block builder consumes it,
// so an offline leader only delays the block: rounds are empty notarized until an online leader claims it.
type pendingBlockSignal struct {
	lock    sync.Mutex
	pending bool
	ch      chan struct{}
}

func newPendingBlockSignal() *pendingBlockSignal {
	return &pendingBlockSignal{ch: make(chan struct{})}
}

// pendingBlockSignal is the network's shared mempool. It stays pending until a block builder consumes it,
// so calling addPendingBlock will always produce a block.
// If the current leader is offline, rounds are empty notarized until an online leader claims it.
func (s *pendingBlockSignal) addPendingBlock() {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.pending = true
	close(s.ch)
	s.ch = make(chan struct{})
}

// wait returns once a block is pending or ctx is cancelled. It does not
// consume the block for block building.
func (s *pendingBlockSignal) wait(ctx context.Context) {
	for {
		s.lock.Lock()
		pending, ch := s.pending, s.ch
		s.lock.Unlock()

		if pending {
			return
		}

		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}

// consume waits for a pending block and claims it, reporting false if ctx is cancelled first.
// called from the block builder.
func (s *pendingBlockSignal) consume(ctx context.Context) bool {
	for {
		s.lock.Lock()
		ch := s.ch
		if s.pending {
			s.pending = false
			s.lock.Unlock()
			return true
		}
		s.lock.Unlock()

		select {
		case <-ch:
		case <-ctx.Done():
			return false
		}
	}
}

// blockBuilderVM builds an inner block only when the test arms a pending block, so the
// chain grows one block per index call.
type blockBuilderVM struct {
	storage *testStorage
	pending *pendingBlockSignal
	bd      testInnerBlockDeserializer
}

func newBlockBuilderVM(storage *testStorage, pending *pendingBlockSignal) *blockBuilderVM {
	return &blockBuilderVM{storage: storage, pending: pending}
}

func (vm *blockBuilderVM) BuildBlock(ctx context.Context, pChainHeight uint64) (avalanchego.VMBlock, error) {
	// Claiming the pending block is what gates block building.
	if !vm.pending.consume(ctx) {
		return nil, ctx.Err()
	}

	// the inner height is the seq of the block being built, which is how many blocks the node
	// has committed so far
	height := vm.storage.NumBlocks()
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, height)
	return &testInnerBlock{Height_: height, TS: time.Now(), Payload: payload}, nil
}

func (vm *blockBuilderVM) ParseBlock(ctx context.Context, bytes []byte) (avalanchego.VMBlock, error) {
	return vm.bd.ParseBlock(ctx, bytes)
}

// WaitForPendingBlock returns while a block is pending, or when ctx is cancelled.
func (vm *blockBuilderVM) WaitForPendingBlock(ctx context.Context) {
	vm.pending.wait(ctx)
}

// noopICMTransition keeps every block in the same ICM epoch, so an epoch only ever changes
// because the validator set did.
func noopICMTransition(_ metadata.ICMEpochInput) metadata.ICMEpochInfo {
	return metadata.ICMEpochInfo{}
}

type node struct {
	t       *testing.T
	stopped bool
	net     *network
	id      common.NodeID
	vm      *blockBuilderVM
	inst    *Instance
	storage *testStorage
	comm    *instanceComm
	wals    *walCreator
}

// stop stops the instance, then drains the messages the node was sending.
func (n *node) stop() {
	if n.stopped {
		return
	}
	n.inst.Stop()
	n.comm.stop()
	n.stopped = true
}

// walRecords returns the records of every WAL file the node currently holds.
func (n *node) walRecords() [][]byte {
	n.wals.lock.Lock()
	defer n.wals.lock.Unlock()
	var records [][]byte
	for _, w := range n.wals.wals {
		entries, err := w.ReadAll()
		require.NoError(n.t, err)
		records = append(records, entries...)
	}
	return records
}

// restart restarts the node with the configuration its instance was started with.
func (n *node) restart() *node {
	params := n.inst.Config.ParameterConfig
	return n.restartWithNodeConfig(nodeConfig{
		params:              &params,
		lastNonSimplexBlock: n.inst.Config.LastNonSimplexInnerBlock,
	})
}

// restartWithNodeConfig restarts the node like restart, but with the given config.
// The node always comes back up over its own storage and WAL, so the storage, wals and existingNode
// fields of the given config are ignored.
func (n *node) restartWithNodeConfig(cfg nodeConfig) *node {
	n.stop()

	records := n.walRecords()

	// Come back up over the same storage, restoring a WAL rebuilt from the exported records.
	restoredWAL := testutil.NewTestWAL(n.t)
	for _, record := range records {
		require.NoError(n.t, restoredWAL.Append(record))
	}

	cfg.wals = []wal.DeletableWAL{restoredWAL}
	cfg.storage = n.storage
	cfg.existingNode = true
	return n.net.addNodeWithConfig(n.id, cfg)
}

// role reports whether the instance currently runs a validator epoch rather than a
// non-validator, and whether it has finished replicating epochs.
func (n *node) role() (isValidator bool, epochsReplicated bool) {
	n.inst.lock.Lock()
	defer n.inst.lock.Unlock()

	if n.inst.e != nil {
		return true, true
	}
	return false, n.inst.nv != nil && n.inst.nv.HasReplicatedEpochs()
}

// sync syncs a node by waiting for the commit of the latest sequence.
func (n *node) sync() *node {
	n.storage.WaitForBlockCommit(n.net.seq - 1)
	return n
}

const firstEverEpoch uint64 = 1

type network struct {
	t *testing.T

	pChain            *testPlatformChain
	seq               uint64
	epoch             uint64
	epochValidatorSet common.Nodes

	// pending holds the block the network has been asked to build, claimable by any leader.
	pending *pendingBlockSignal

	// lock guards nodes and offline, which comm goroutines read while tests mutate them.
	lock  sync.Mutex
	nodes []node
	// offline nodes stay in the network but neither send nor receive messages.
	offline map[string]struct{}
}

func (n *network) nodesSnapshot() []node {
	n.lock.Lock()
	defer n.lock.Unlock()
	return append([]node(nil), n.nodes...)
}

func (n *network) setOffline(id common.NodeID) {
	n.lock.Lock()
	defer n.lock.Unlock()
	n.offline[string(id)] = struct{}{}
}

func (n *network) setOnline(id common.NodeID) {
	n.lock.Lock()
	defer n.lock.Unlock()
	delete(n.offline, string(id))
}

func (n *network) isOffline(id common.NodeID) bool {
	n.lock.Lock()
	defer n.lock.Unlock()
	_, offline := n.offline[string(id)]
	return offline
}

func newNetwork(t *testing.T, pChain *testPlatformChain) *network {
	genesisNodes := pChain.GenesisValidatorSet().Nodes()
	common.SortNodes(genesisNodes)

	return &network{
		t:                 t,
		pChain:            pChain,
		pending:           newPendingBlockSignal(),
		offline:           make(map[string]struct{}),
		epochValidatorSet: genesisNodes,

		// Genesis at seq 0. Then first simplex block is built automatically
		// without a build block notification
		seq:   2,
		epoch: firstEverEpoch,
	}
}

// nodeConfig holds optional overrides for a node added to the network.
type nodeConfig struct {
	// storage the node starts from; defaults to a fresh storage holding only genesis.
	storage *testStorage
	// wals are pre-existing WALs the instance restores on start.
	wals                []wal.DeletableWAL
	lastNonSimplexBlock avalanchego.VMBlock
	// existingNode indicates whether the node is being added to the network for the first time (false) or is a restart of an existing node (true).
	existingNode bool
	// params overrides the shared paramConfig the instance is configured with, when non-nil.
	params *ParameterConfig
}

// addNode creates and starts a node in the network.
func (n *network) addNode(id common.NodeID) *node {
	return n.addNodeWithConfig(id, nodeConfig{})
}

// addNodeWithConfig creates and starts a node built from the given config.
func (n *network) addNodeWithConfig(id common.NodeID, cfg nodeConfig) *node {
	storage := cfg.storage
	if storage == nil {
		storage = newTestStorageWithGenesis(n.t)
	}

	comm := newInstanceComm(n, id)

	vm := newBlockBuilderVM(storage, n.pending)
	wc := &walCreator{t: n.t}
	var lastNonSimplex avalanchego.VMBlock = genesisBlock
	if cfg.lastNonSimplexBlock != nil {
		lastNonSimplex = cfg.lastNonSimplexBlock
	}
	params := paramConfig
	if cfg.params != nil {
		params = *cfg.params
	}

	instance := NewInstance(Config{
		LastNonSimplexInnerBlock: lastNonSimplex,
		ParameterConfig:          params,
		PlatformChain:            n.pChain,
		Broadcaster:              comm,
		Sender:                   comm,
		CryptoOps:                &testCryptoOps{},
		WalCreator:               wc.createWAL,
		Storage:                  storage,
		// the first byte of the node id labels the node's log records
		Logger:         testutil.MakeLogger(n.t, int(id[0])),
		WALs:           cfg.wals,
		VM:             vm,
		ICMETransition: noopICMTransition,
		ID:             id,
	})

	node := node{
		t:       n.t,
		net:     n,
		id:      id,
		storage: storage,
		comm:    comm,
		vm:      vm,
		inst:    instance,
		wals:    wc,
	}

	n.lock.Lock()
	if cfg.existingNode {
		for i, existingNode := range n.nodes {
			if existingNode.id.Equals(id) {
				n.nodes[i] = node
				break
			}
		}
	} else {
		// ensure a unique id; snapshot because nodes may be added concurrently
		for _, node := range n.nodes {
			require.NotEqual(n.t, node.id, id)
		}
		n.nodes = append(n.nodes, node)
	}
	n.lock.Unlock()

	instance.Config.Logger.Debug("Created a node in the test network", zap.Uint64("Seq", n.seq), zap.Uint64("num block", node.storage.NumBlocks()))

	ctx, cancel := context.WithCancel(context.Background())
	n.t.Cleanup(cancel)

	comm.start()
	require.NoError(n.t, instance.Start(ctx))
	n.t.Cleanup(node.stop)

	return &node
}

// waitUntilValidatorsRunning blocks until every node that should be validator in the current epoch
// is actually a validator. This ensures that acceptNewBlock, waits for any nodes that may be transitioning from
// a non-validator actually here about the new proposal.
func (n *network) waitUntilValidatorsReady() {
	for _, node := range n.nodesSnapshot() {
		if common.NodeIDs(n.epochValidatorSet.NodeIDs()).IndexOf(node.id) < 0 {
			continue
		}

		require.Eventually(n.t, func() bool {
			isValidator, _ := node.role()
			return isValidator
		}, time.Minute, time.Millisecond, "node %x never started running a validator", node.id)
	}
}

// sync blocks until every node has committed the block at the network's tip, requiring
// that they all committed the same block and that it sits at n.seq - 1.
func (n *network) sync() {
	tip := n.seq - 1

	var block common.VerifiedBlock
	for _, node := range n.nodesSnapshot() {
		committedBlock := node.storage.WaitForBlockCommit(tip)
		if block == nil {
			block = committedBlock
			continue
		}

		require.Equal(n.t, block.Bytes(), committedBlock.Bytes())
	}

	require.Equal(n.t, tip, block.BlockHeader().Seq)
}

// acceptNewBlock blocks until every online node has accepted a newly indexed block.
func (n *network) acceptNewBlock() (common.VerifiedBlock, common.Finalization) {
	// no nodes have indexed this sequence yet
	for _, node := range n.nodes {
		_, _, err := node.storage.Retrieve(n.seq)
		require.ErrorIs(n.t, err, common.ErrBlockNotFound)
	}

	n.waitUntilValidatorsReady()

	// Adds a pending block to the network, to be built by the next online node.
	n.pending.addPendingBlock()

	var block common.VerifiedBlock
	var finalization common.Finalization
	for _, node := range n.nodes {
		if n.isOffline(node.id) {
			continue
		}
		committedBlock := node.storage.WaitForBlockCommit(n.seq)
		if block == nil {
			_, fin, err := node.storage.Retrieve(n.seq)
			require.NoError(n.t, err)
			finalization = fin
			block = committedBlock
		} else {
			require.Equal(n.t, block.Bytes(), committedBlock.Bytes())
		}
	}

	require.Equal(n.t, block.BlockHeader().Seq, n.seq)
	n.seq++

	// check if its a sealing
	if block.SealingBlockInfo() != nil {
		n.epoch = n.seq
		newValidatorSet := block.SealingBlockInfo().ValidatorSet
		common.SortNodes(newValidatorSet)
		n.epochValidatorSet = newValidatorSet
	}

	return block, finalization
}

// waitUntilSealingBlock waits until every node commits the block at the current seq,
// repeating until that block is a sealing block. It then advances the network into
// the new epoch and returns the sealing block.
// This is useful for when we are transitioning epochs because blocks will be built impatiently
// without a notification from the mempool.
func (n *network) waitUntilSealingBlock(expectedValidatorSet common.Nodes) common.VerifiedBlock {
	for {
		var block common.VerifiedBlock
		for _, node := range n.nodes {
			if n.isOffline(node.id) {
				continue
			}
			committedBlock := node.storage.WaitForBlockCommit(n.seq)
			if block == nil {
				block = committedBlock
			} else {
				require.Equal(n.t, block.Bytes(), committedBlock.Bytes())
			}
		}

		require.Equal(n.t, block.BlockHeader().Seq, n.seq)
		n.seq++

		if block.SealingBlockInfo() == nil {
			continue
		}

		// add the validator set to the networks memory for block building
		n.epoch = n.seq
		newValidatorSet := block.SealingBlockInfo().ValidatorSet
		assertExpectedNodeIds(n.t, expectedValidatorSet.NodeIDs(), newValidatorSet.NodeIDs())
		common.SortNodes(newValidatorSet)
		n.epochValidatorSet = newValidatorSet
		return block
	}
}

// newNodeMapping creates a mapping with a nodeID, BLSKey and Weight with a given [id].
// id is passed as an int for consistent logs between runs.
func newNodeMapping(id int) metadata.NodeBLSMapping {
	avaID := [20]byte{byte(id)}

	return metadata.NodeBLSMapping{
		NodeID: avalanchego.NodeID(avaID),
		BLSKey: []byte{avaID[0], byte(id + 1)},
		Weight: 1,
	}
}

// assertExpectedNodeIds asserts the validator set contains exactly the expected node IDs.
func assertExpectedNodeIds(t *testing.T, validatorSet common.NodeIDs, expected common.NodeIDs) {
	require.ElementsMatch(t, expected, validatorSet)
}
