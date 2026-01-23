//! Network benchmarks.
//!
//! Benchmarks for P2P networking, message serialization, and gossip protocols.

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use criterion::{black_box, criterion_group, criterion_main, Criterion, BenchmarkId, Throughput};

use avalanche_ids::{Id, NodeId};

/// Mock message for benchmarking.
#[derive(Clone)]
struct BenchMessage {
    msg_type: u8,
    chain_id: Id,
    request_id: u32,
    deadline: u64,
    payload: Vec<u8>,
}

impl BenchMessage {
    fn new(msg_type: u8, payload_size: usize) -> Self {
        Self {
            msg_type,
            chain_id: Id::from_bytes([0; 32]),
            request_id: 1,
            deadline: 0,
            payload: vec![0; payload_size],
        }
    }

    fn serialize(&self) -> Vec<u8> {
        let mut bytes = Vec::with_capacity(1 + 32 + 4 + 8 + self.payload.len());
        bytes.push(self.msg_type);
        bytes.extend_from_slice(self.chain_id.as_bytes());
        bytes.extend_from_slice(&self.request_id.to_be_bytes());
        bytes.extend_from_slice(&self.deadline.to_be_bytes());
        bytes.extend_from_slice(&self.payload);
        bytes
    }

    fn deserialize(bytes: &[u8]) -> Option<Self> {
        if bytes.len() < 45 {
            return None;
        }

        Some(Self {
            msg_type: bytes[0],
            chain_id: Id::from_slice(&bytes[1..33]).ok()?,
            request_id: u32::from_be_bytes(bytes[33..37].try_into().ok()?),
            deadline: u64::from_be_bytes(bytes[37..45].try_into().ok()?),
            payload: bytes[45..].to_vec(),
        })
    }
}

/// Mock peer for benchmarking.
struct BenchPeer {
    node_id: NodeId,
    send_buffer: Vec<BenchMessage>,
    recv_buffer: Vec<BenchMessage>,
}

impl BenchPeer {
    fn new(node_id: NodeId) -> Self {
        Self {
            node_id,
            send_buffer: Vec::new(),
            recv_buffer: Vec::new(),
        }
    }

    fn send(&mut self, msg: BenchMessage) {
        self.send_buffer.push(msg);
    }

    fn receive(&mut self, msg: BenchMessage) {
        self.recv_buffer.push(msg);
    }

    fn flush(&mut self) -> Vec<BenchMessage> {
        std::mem::take(&mut self.send_buffer)
    }
}

/// Mock network for benchmarking.
struct BenchNetwork {
    peers: HashMap<NodeId, BenchPeer>,
    local_id: NodeId,
}

impl BenchNetwork {
    fn new(num_peers: usize) -> Self {
        let local_id = NodeId::from_bytes([0; 20]);
        let mut peers = HashMap::new();

        for i in 0..num_peers {
            let mut id_bytes = [0u8; 20];
            id_bytes[0..4].copy_from_slice(&(i as u32 + 1).to_be_bytes());
            let node_id = NodeId::from_bytes(id_bytes);
            peers.insert(node_id, BenchPeer::new(node_id));
        }

        Self { peers, local_id }
    }

    fn send(&mut self, peer_id: NodeId, msg: BenchMessage) -> bool {
        if let Some(peer) = self.peers.get_mut(&peer_id) {
            peer.send(msg);
            true
        } else {
            false
        }
    }

    fn broadcast(&mut self, msg: BenchMessage) -> usize {
        let peer_ids: Vec<_> = self.peers.keys().copied().collect();
        let mut sent = 0;
        for peer_id in peer_ids {
            if self.send(peer_id, msg.clone()) {
                sent += 1;
            }
        }
        sent
    }

    fn gossip(&mut self, msg: BenchMessage, fanout: usize) -> HashSet<NodeId> {
        let peer_ids: Vec<_> = self.peers.keys().copied().collect();
        let mut sent_to = HashSet::new();

        // Simple random gossip
        let seed = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos() as usize;

        for i in 0..fanout.min(peer_ids.len()) {
            let idx = (seed + i * 7) % peer_ids.len();
            let peer_id = peer_ids[idx];
            if self.send(peer_id, msg.clone()) {
                sent_to.insert(peer_id);
            }
        }

        sent_to
    }

    fn peer_count(&self) -> usize {
        self.peers.len()
    }
}

fn bench_message_serialization(c: &mut Criterion) {
    let mut group = c.benchmark_group("message_serialization");

    for payload_size in [64, 256, 1024, 4096, 16384].iter() {
        let msg = BenchMessage::new(1, *payload_size);

        group.throughput(Throughput::Bytes(*payload_size as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(payload_size),
            &msg,
            |b, msg| {
                b.iter(|| black_box(msg.serialize()));
            },
        );
    }

    group.finish();
}

fn bench_message_deserialization(c: &mut Criterion) {
    let mut group = c.benchmark_group("message_deserialization");

    for payload_size in [64, 256, 1024, 4096, 16384].iter() {
        let msg = BenchMessage::new(1, *payload_size);
        let bytes = msg.serialize();

        group.throughput(Throughput::Bytes(bytes.len() as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(payload_size),
            &bytes,
            |b, bytes| {
                b.iter(|| black_box(BenchMessage::deserialize(bytes)));
            },
        );
    }

    group.finish();
}

fn bench_unicast(c: &mut Criterion) {
    let mut group = c.benchmark_group("unicast");

    for num_peers in [10, 50, 100, 500].iter() {
        let mut network = BenchNetwork::new(*num_peers);
        let msg = BenchMessage::new(1, 256);
        let peer_ids: Vec<_> = network.peers.keys().copied().collect();

        group.throughput(Throughput::Elements(1));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_peers),
            &peer_ids,
            |b, ids| {
                b.iter(|| {
                    let idx = ids.len() / 2;
                    network.send(ids[idx], msg.clone());
                    black_box(true)
                });
            },
        );
    }

    group.finish();
}

fn bench_broadcast(c: &mut Criterion) {
    let mut group = c.benchmark_group("broadcast");

    for num_peers in [10, 50, 100, 500].iter() {
        let mut network = BenchNetwork::new(*num_peers);
        let msg = BenchMessage::new(1, 256);

        group.throughput(Throughput::Elements(*num_peers as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_peers),
            num_peers,
            |b, _| {
                b.iter(|| black_box(network.broadcast(msg.clone())));
            },
        );
    }

    group.finish();
}

fn bench_gossip(c: &mut Criterion) {
    let mut group = c.benchmark_group("gossip");

    // Test different fanout values with fixed peer count
    let num_peers = 100;
    for fanout in [4, 8, 16, 32].iter() {
        let mut network = BenchNetwork::new(num_peers);
        let msg = BenchMessage::new(1, 256);

        group.throughput(Throughput::Elements(*fanout as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(fanout),
            fanout,
            |b, &fanout| {
                b.iter(|| black_box(network.gossip(msg.clone(), fanout)));
            },
        );
    }

    group.finish();
}

fn bench_peer_lookup(c: &mut Criterion) {
    let mut group = c.benchmark_group("peer_lookup");

    for num_peers in [100, 500, 1000, 5000].iter() {
        let network = BenchNetwork::new(*num_peers);
        let peer_ids: Vec<_> = network.peers.keys().copied().collect();

        group.throughput(Throughput::Elements(1));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_peers),
            &peer_ids,
            |b, ids| {
                b.iter(|| {
                    let idx = ids.len() / 2;
                    black_box(network.peers.get(&ids[idx]))
                });
            },
        );
    }

    group.finish();
}

fn bench_node_id_operations(c: &mut Criterion) {
    let mut group = c.benchmark_group("node_id_ops");

    // Creation
    group.bench_function("creation", |b| {
        b.iter(|| {
            let bytes = [1u8; 20];
            black_box(NodeId::from_bytes(bytes))
        });
    });

    // Comparison
    let id1 = NodeId::from_bytes([1; 20]);
    let id2 = NodeId::from_bytes([2; 20]);
    group.bench_function("comparison", |b| {
        b.iter(|| black_box(id1 == id2));
    });

    // Hashing (for HashMap)
    group.bench_function("hashing", |b| {
        use std::hash::{Hash, Hasher};
        use std::collections::hash_map::DefaultHasher;

        let id = NodeId::from_bytes([1; 20]);
        b.iter(|| {
            let mut hasher = DefaultHasher::new();
            id.hash(&mut hasher);
            black_box(hasher.finish())
        });
    });

    group.finish();
}

fn bench_message_routing(c: &mut Criterion) {
    // Simulate message routing based on chain ID
    let mut group = c.benchmark_group("message_routing");

    for num_chains in [3, 10, 50].iter() {
        // Create routing table
        let routing_table: HashMap<Id, u32> = (0..*num_chains)
            .map(|i| {
                let mut id_bytes = [0u8; 32];
                id_bytes[0..4].copy_from_slice(&(i as u32).to_be_bytes());
                (Id::from_bytes(id_bytes), i as u32)
            })
            .collect();

        let test_id = {
            let mut id_bytes = [0u8; 32];
            id_bytes[0..4].copy_from_slice(&(num_chains / 2).to_be_bytes());
            Id::from_bytes(id_bytes)
        };

        group.throughput(Throughput::Elements(1));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_chains),
            &routing_table,
            |b, table| {
                b.iter(|| black_box(table.get(&test_id)));
            },
        );
    }

    group.finish();
}

fn bench_connection_pool(c: &mut Criterion) {
    // Simulate connection pool operations
    let mut group = c.benchmark_group("connection_pool");

    for pool_size in [50, 100, 500, 1000].iter() {
        let mut pool: HashMap<NodeId, bool> = (0..*pool_size)
            .map(|i| {
                let mut bytes = [0u8; 20];
                bytes[0..4].copy_from_slice(&(i as u32).to_be_bytes());
                (NodeId::from_bytes(bytes), true)
            })
            .collect();

        // Test add/remove operations
        group.bench_with_input(
            BenchmarkId::from_parameter(pool_size),
            pool_size,
            |b, _| {
                let new_id = NodeId::from_bytes([255; 20]);
                b.iter(|| {
                    pool.insert(new_id, true);
                    pool.remove(&new_id);
                });
            },
        );
    }

    group.finish();
}

fn bench_bandwidth_tracking(c: &mut Criterion) {
    // Simulate bandwidth tracking per peer
    struct BandwidthTracker {
        usage: HashMap<NodeId, u64>,
    }

    impl BandwidthTracker {
        fn new(num_peers: usize) -> Self {
            let usage = (0..num_peers)
                .map(|i| {
                    let mut bytes = [0u8; 20];
                    bytes[0..4].copy_from_slice(&(i as u32).to_be_bytes());
                    (NodeId::from_bytes(bytes), 0u64)
                })
                .collect();
            Self { usage }
        }

        fn record(&mut self, peer: NodeId, bytes: u64) {
            *self.usage.entry(peer).or_insert(0) += bytes;
        }

        fn get(&self, peer: &NodeId) -> u64 {
            *self.usage.get(peer).unwrap_or(&0)
        }
    }

    let mut tracker = BandwidthTracker::new(100);
    let peer = NodeId::from_bytes([50; 20]);
    tracker.usage.insert(peer, 0);

    c.bench_function("bandwidth_tracking", |b| {
        b.iter(|| {
            tracker.record(peer, 1024);
            black_box(tracker.get(&peer))
        });
    });
}

fn bench_message_batching(c: &mut Criterion) {
    let mut group = c.benchmark_group("message_batching");

    for batch_size in [10, 50, 100, 500].iter() {
        let messages: Vec<BenchMessage> = (0..*batch_size)
            .map(|_| BenchMessage::new(1, 256))
            .collect();

        group.throughput(Throughput::Elements(*batch_size as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(batch_size),
            &messages,
            |b, msgs| {
                b.iter(|| {
                    // Serialize batch
                    let mut batch = Vec::with_capacity(msgs.len() * 300);
                    for msg in msgs {
                        let serialized = msg.serialize();
                        batch.extend_from_slice(&(serialized.len() as u32).to_be_bytes());
                        batch.extend_from_slice(&serialized);
                    }
                    black_box(batch)
                });
            },
        );
    }

    group.finish();
}

criterion_group!(
    benches,
    bench_message_serialization,
    bench_message_deserialization,
    bench_unicast,
    bench_broadcast,
    bench_gossip,
    bench_peer_lookup,
    bench_node_id_operations,
    bench_message_routing,
    bench_connection_pool,
    bench_bandwidth_tracking,
    bench_message_batching,
);

criterion_main!(benches);
