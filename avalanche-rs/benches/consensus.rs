//! Consensus benchmarks.
//!
//! Benchmarks for Snowball and Snowman consensus protocols.

use std::collections::HashMap;
use std::sync::Arc;

use criterion::{black_box, criterion_group, criterion_main, Criterion, BenchmarkId, Throughput};

use avalanche_ids::{Id, NodeId};

/// Simple Snowball state for benchmarking.
struct SnowballBench {
    preference: Id,
    last_preference: Id,
    confidence: u32,
    consecutive_successes: u32,
    k: u32,
    alpha: u32,
    beta: u32,
    finalized: bool,
}

impl SnowballBench {
    fn new(k: u32, alpha: u32, beta: u32) -> Self {
        Self {
            preference: Id::default(),
            last_preference: Id::default(),
            confidence: 0,
            consecutive_successes: 0,
            k,
            alpha,
            beta,
            finalized: false,
        }
    }

    fn record_poll(&mut self, votes: &HashMap<Id, u32>) {
        if self.finalized {
            return;
        }

        // Find choice with most votes
        let (best_choice, best_count) = votes
            .iter()
            .max_by_key(|(_, count)| *count)
            .map(|(id, count)| (*id, *count))
            .unwrap_or((Id::default(), 0));

        if best_count >= self.alpha {
            // Successful poll
            if best_choice == self.preference {
                self.consecutive_successes += 1;
            } else {
                self.consecutive_successes = 1;
                self.preference = best_choice;
            }

            if best_choice == self.last_preference {
                self.confidence += 1;
            } else {
                self.confidence = 1;
                self.last_preference = best_choice;
            }

            if self.consecutive_successes >= self.beta {
                self.finalized = true;
            }
        } else {
            self.consecutive_successes = 0;
        }
    }

    fn is_finalized(&self) -> bool {
        self.finalized
    }
}

/// Mock block for Snowman benchmarking.
#[derive(Clone)]
struct BenchBlock {
    id: Id,
    parent_id: Id,
    height: u64,
    bytes: Vec<u8>,
}

impl BenchBlock {
    fn new(height: u64, parent_id: Id) -> Self {
        let mut id_bytes = [0u8; 32];
        id_bytes[0..8].copy_from_slice(&height.to_be_bytes());

        Self {
            id: Id::from_bytes(id_bytes),
            parent_id,
            height,
            bytes: vec![0; 256],
        }
    }

    fn genesis() -> Self {
        Self::new(0, Id::default())
    }
}

/// Snowman chain state for benchmarking.
struct SnowmanBench {
    blocks: HashMap<Id, BenchBlock>,
    snowball: SnowballBench,
    last_accepted: Id,
    preferred: Id,
}

impl SnowmanBench {
    fn new(k: u32, alpha: u32, beta: u32) -> Self {
        let genesis = BenchBlock::genesis();
        let genesis_id = genesis.id;

        let mut blocks = HashMap::new();
        blocks.insert(genesis_id, genesis);

        Self {
            blocks,
            snowball: SnowballBench::new(k, alpha, beta),
            last_accepted: genesis_id,
            preferred: genesis_id,
        }
    }

    fn add_block(&mut self, block: BenchBlock) {
        self.blocks.insert(block.id, block);
    }

    fn build_block(&mut self) -> BenchBlock {
        let parent = self.blocks.get(&self.preferred).unwrap();
        let block = BenchBlock::new(parent.height + 1, self.preferred);
        let id = block.id;
        self.blocks.insert(id, block.clone());
        self.preferred = id;
        block
    }

    fn accept_block(&mut self, id: Id) {
        self.last_accepted = id;
    }
}

fn bench_snowball_poll(c: &mut Criterion) {
    let mut group = c.benchmark_group("snowball_poll");

    for sample_size in [20, 40, 80].iter() {
        group.throughput(Throughput::Elements(*sample_size as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(sample_size),
            sample_size,
            |b, &size| {
                let mut snowball = SnowballBench::new(size, size * 3 / 4, 20);
                let choice = Id::from_bytes([1; 32]);

                b.iter(|| {
                    let mut votes = HashMap::new();
                    votes.insert(choice, size);
                    snowball.record_poll(black_box(&votes));
                });
            },
        );
    }

    group.finish();
}

fn bench_snowball_finalization(c: &mut Criterion) {
    c.bench_function("snowball_finalization", |b| {
        b.iter(|| {
            let mut snowball = SnowballBench::new(20, 15, 20);
            let choice = Id::from_bytes([1; 32]);

            let mut votes = HashMap::new();
            votes.insert(choice, 20);

            while !snowball.is_finalized() {
                snowball.record_poll(&votes);
            }

            black_box(snowball.is_finalized())
        });
    });
}

fn bench_block_creation(c: &mut Criterion) {
    c.bench_function("block_creation", |b| {
        let mut snowman = SnowmanBench::new(20, 15, 20);

        b.iter(|| {
            let block = snowman.build_block();
            black_box(block)
        });
    });
}

fn bench_block_lookup(c: &mut Criterion) {
    let mut group = c.benchmark_group("block_lookup");

    for num_blocks in [100, 1000, 10000].iter() {
        // Setup: create blocks
        let mut snowman = SnowmanBench::new(20, 15, 20);
        let mut block_ids = Vec::new();

        for _ in 0..*num_blocks {
            let block = snowman.build_block();
            block_ids.push(block.id);
            snowman.accept_block(block.id);
        }

        group.throughput(Throughput::Elements(1));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_blocks),
            &block_ids,
            |b, ids| {
                let idx = ids.len() / 2;
                let id = ids[idx];

                b.iter(|| black_box(snowman.blocks.get(&id)));
            },
        );
    }

    group.finish();
}

fn bench_chain_building(c: &mut Criterion) {
    let mut group = c.benchmark_group("chain_building");

    for num_blocks in [10, 100, 1000].iter() {
        group.throughput(Throughput::Elements(*num_blocks as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_blocks),
            num_blocks,
            |b, &n| {
                b.iter(|| {
                    let mut snowman = SnowmanBench::new(20, 15, 20);
                    for _ in 0..n {
                        let block = snowman.build_block();
                        snowman.accept_block(block.id);
                    }
                    black_box(snowman.last_accepted)
                });
            },
        );
    }

    group.finish();
}

fn bench_validator_sampling(c: &mut Criterion) {
    let mut group = c.benchmark_group("validator_sampling");

    for num_validators in [100, 500, 1000, 5000].iter() {
        let validators: Vec<NodeId> = (0..*num_validators)
            .map(|i| {
                let mut bytes = [0u8; 20];
                bytes[0..4].copy_from_slice(&(i as u32).to_be_bytes());
                NodeId::from_bytes(bytes)
            })
            .collect();

        group.throughput(Throughput::Elements(20)); // Sample size
        group.bench_with_input(
            BenchmarkId::from_parameter(num_validators),
            &validators,
            |b, vals| {
                b.iter(|| {
                    // Simple random sampling (not cryptographically secure)
                    let seed = std::time::SystemTime::now()
                        .duration_since(std::time::UNIX_EPOCH)
                        .unwrap()
                        .as_nanos() as usize;

                    let sample: Vec<&NodeId> = (0..20)
                        .map(|i| &vals[(seed + i * 7) % vals.len()])
                        .collect();

                    black_box(sample)
                });
            },
        );
    }

    group.finish();
}

fn bench_id_hashing(c: &mut Criterion) {
    use sha2::{Digest, Sha256};

    c.bench_function("id_hashing", |b| {
        let data = vec![0u8; 256];

        b.iter(|| {
            let hash = Sha256::digest(black_box(&data));
            black_box(Id::from_slice(&hash))
        });
    });
}

fn bench_block_serialization(c: &mut Criterion) {
    let mut group = c.benchmark_group("block_serialization");

    for block_size in [256, 1024, 4096, 16384].iter() {
        let block = BenchBlock {
            id: Id::from_bytes([1; 32]),
            parent_id: Id::from_bytes([0; 32]),
            height: 100,
            bytes: vec![0u8; *block_size],
        };

        group.throughput(Throughput::Bytes(*block_size as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(block_size),
            &block,
            |b, block| {
                b.iter(|| {
                    // Simulate serialization
                    let mut output = Vec::with_capacity(32 + 32 + 8 + block.bytes.len());
                    output.extend_from_slice(block.id.as_bytes());
                    output.extend_from_slice(block.parent_id.as_bytes());
                    output.extend_from_slice(&block.height.to_be_bytes());
                    output.extend_from_slice(&block.bytes);
                    black_box(output)
                });
            },
        );
    }

    group.finish();
}

fn bench_concurrent_polls(c: &mut Criterion) {
    use std::sync::Mutex;

    c.bench_function("concurrent_polls", |b| {
        let snowball = Arc::new(Mutex::new(SnowballBench::new(20, 15, 20)));
        let choice = Id::from_bytes([1; 32]);

        b.iter(|| {
            let handles: Vec<_> = (0..4)
                .map(|_| {
                    let sb = snowball.clone();
                    std::thread::spawn(move || {
                        let mut votes = HashMap::new();
                        votes.insert(choice, 20);
                        let mut guard = sb.lock().unwrap();
                        guard.record_poll(&votes);
                    })
                })
                .collect();

            for handle in handles {
                handle.join().unwrap();
            }
        });
    });
}

criterion_group!(
    benches,
    bench_snowball_poll,
    bench_snowball_finalization,
    bench_block_creation,
    bench_block_lookup,
    bench_chain_building,
    bench_validator_sampling,
    bench_id_hashing,
    bench_block_serialization,
    bench_concurrent_polls,
);

criterion_main!(benches);
