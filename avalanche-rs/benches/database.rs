//! Database benchmarks.
//!
//! Benchmarks for database operations, state management, and caching.

use std::collections::HashMap;
use std::sync::Arc;

use criterion::{black_box, criterion_group, criterion_main, Criterion, BenchmarkId, Throughput};
use parking_lot::RwLock;

use avalanche_ids::Id;

/// In-memory database for benchmarking.
struct BenchDB {
    data: RwLock<HashMap<Vec<u8>, Vec<u8>>>,
}

impl BenchDB {
    fn new() -> Self {
        Self {
            data: RwLock::new(HashMap::new()),
        }
    }

    fn get(&self, key: &[u8]) -> Option<Vec<u8>> {
        self.data.read().get(key).cloned()
    }

    fn put(&self, key: Vec<u8>, value: Vec<u8>) {
        self.data.write().insert(key, value);
    }

    fn delete(&self, key: &[u8]) {
        self.data.write().remove(key);
    }

    fn has(&self, key: &[u8]) -> bool {
        self.data.read().contains_key(key)
    }

    fn batch_put(&self, items: Vec<(Vec<u8>, Vec<u8>)>) {
        let mut data = self.data.write();
        for (key, value) in items {
            data.insert(key, value);
        }
    }

    fn iterate_prefix(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        self.data
            .read()
            .iter()
            .filter(|(k, _)| k.starts_with(prefix))
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect()
    }
}

/// LRU cache for benchmarking.
struct LRUCache {
    capacity: usize,
    data: HashMap<Id, Vec<u8>>,
    order: Vec<Id>,
}

impl LRUCache {
    fn new(capacity: usize) -> Self {
        Self {
            capacity,
            data: HashMap::with_capacity(capacity),
            order: Vec::with_capacity(capacity),
        }
    }

    fn get(&mut self, key: &Id) -> Option<&Vec<u8>> {
        if self.data.contains_key(key) {
            // Move to front of order
            self.order.retain(|k| k != key);
            self.order.insert(0, *key);
            self.data.get(key)
        } else {
            None
        }
    }

    fn put(&mut self, key: Id, value: Vec<u8>) {
        if self.data.len() >= self.capacity && !self.data.contains_key(&key) {
            // Evict oldest
            if let Some(oldest) = self.order.pop() {
                self.data.remove(&oldest);
            }
        }

        self.order.retain(|k| k != &key);
        self.order.insert(0, key);
        self.data.insert(key, value);
    }

    fn contains(&self, key: &Id) -> bool {
        self.data.contains_key(key)
    }
}

/// Block storage for benchmarking.
struct BlockStore {
    db: BenchDB,
    cache: RwLock<LRUCache>,
}

impl BlockStore {
    fn new(cache_size: usize) -> Self {
        Self {
            db: BenchDB::new(),
            cache: RwLock::new(LRUCache::new(cache_size)),
        }
    }

    fn put_block(&self, id: Id, data: Vec<u8>) {
        let key = [b"block:", id.as_bytes()].concat();
        self.db.put(key, data.clone());
        self.cache.write().put(id, data);
    }

    fn get_block(&self, id: &Id) -> Option<Vec<u8>> {
        // Check cache first
        {
            let mut cache = self.cache.write();
            if let Some(data) = cache.get(id) {
                return Some(data.clone());
            }
        }

        // Fall back to DB
        let key = [b"block:", id.as_bytes()].concat();
        if let Some(data) = self.db.get(&key) {
            self.cache.write().put(*id, data.clone());
            Some(data)
        } else {
            None
        }
    }

    fn has_block(&self, id: &Id) -> bool {
        if self.cache.read().contains(id) {
            return true;
        }
        let key = [b"block:", id.as_bytes()].concat();
        self.db.has(&key)
    }
}

fn bench_db_put(c: &mut Criterion) {
    let mut group = c.benchmark_group("db_put");

    for value_size in [64, 256, 1024, 4096].iter() {
        let db = BenchDB::new();
        let value = vec![0u8; *value_size];

        group.throughput(Throughput::Bytes(*value_size as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(value_size),
            &value,
            |b, val| {
                let mut counter = 0u64;
                b.iter(|| {
                    let key = counter.to_be_bytes().to_vec();
                    counter += 1;
                    db.put(key, val.clone());
                });
            },
        );
    }

    group.finish();
}

fn bench_db_get(c: &mut Criterion) {
    let mut group = c.benchmark_group("db_get");

    for num_entries in [100, 1000, 10000].iter() {
        let db = BenchDB::new();

        // Prepopulate
        for i in 0..*num_entries {
            let key = (i as u64).to_be_bytes().to_vec();
            let value = vec![0u8; 256];
            db.put(key, value);
        }

        let test_key = (num_entries / 2).to_be_bytes().to_vec();

        group.throughput(Throughput::Elements(1));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_entries),
            &test_key,
            |b, key| {
                b.iter(|| black_box(db.get(key)));
            },
        );
    }

    group.finish();
}

fn bench_db_batch_put(c: &mut Criterion) {
    let mut group = c.benchmark_group("db_batch_put");

    for batch_size in [10, 50, 100, 500].iter() {
        let db = BenchDB::new();
        let items: Vec<(Vec<u8>, Vec<u8>)> = (0..*batch_size)
            .map(|i| ((i as u64).to_be_bytes().to_vec(), vec![0u8; 256]))
            .collect();

        group.throughput(Throughput::Elements(*batch_size as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(batch_size),
            &items,
            |b, items| {
                b.iter(|| db.batch_put(items.clone()));
            },
        );
    }

    group.finish();
}

fn bench_db_iterate_prefix(c: &mut Criterion) {
    let mut group = c.benchmark_group("db_iterate_prefix");

    let db = BenchDB::new();

    // Create entries with different prefixes
    for prefix in [b"block:", b"utxo:", b"tx:"].iter() {
        for i in 0..1000u64 {
            let key = [*prefix, &i.to_be_bytes()[..]].concat();
            db.put(key, vec![0u8; 64]);
        }
    }

    for prefix in [b"block:", b"utxo:", b"tx:"].iter() {
        let prefix_str = String::from_utf8_lossy(*prefix);
        group.bench_with_input(
            BenchmarkId::from_parameter(prefix_str.to_string()),
            prefix,
            |b, prefix| {
                b.iter(|| black_box(db.iterate_prefix(*prefix)));
            },
        );
    }

    group.finish();
}

fn bench_cache_hit(c: &mut Criterion) {
    let mut group = c.benchmark_group("cache_hit");

    for cache_size in [100, 1000, 10000].iter() {
        let mut cache = LRUCache::new(*cache_size);

        // Fill cache
        for i in 0..*cache_size {
            let mut id_bytes = [0u8; 32];
            id_bytes[0..8].copy_from_slice(&(i as u64).to_be_bytes());
            cache.put(Id::from_bytes(id_bytes), vec![0u8; 256]);
        }

        let test_id = {
            let mut id_bytes = [0u8; 32];
            id_bytes[0..8].copy_from_slice(&((cache_size / 2) as u64).to_be_bytes());
            Id::from_bytes(id_bytes)
        };

        group.throughput(Throughput::Elements(1));
        group.bench_with_input(
            BenchmarkId::from_parameter(cache_size),
            &test_id,
            |b, id| {
                b.iter(|| black_box(cache.get(id)));
            },
        );
    }

    group.finish();
}

fn bench_cache_miss(c: &mut Criterion) {
    let mut group = c.benchmark_group("cache_miss");

    for cache_size in [100, 1000, 10000].iter() {
        let mut cache = LRUCache::new(*cache_size);

        // Fill cache
        for i in 0..*cache_size {
            let mut id_bytes = [0u8; 32];
            id_bytes[0..8].copy_from_slice(&(i as u64).to_be_bytes());
            cache.put(Id::from_bytes(id_bytes), vec![0u8; 256]);
        }

        // Test ID not in cache
        let test_id = Id::from_bytes([255; 32]);

        group.throughput(Throughput::Elements(1));
        group.bench_with_input(
            BenchmarkId::from_parameter(cache_size),
            &test_id,
            |b, id| {
                b.iter(|| black_box(cache.get(id)));
            },
        );
    }

    group.finish();
}

fn bench_cache_eviction(c: &mut Criterion) {
    let cache_size = 1000;
    let mut cache = LRUCache::new(cache_size);

    // Fill cache
    for i in 0..cache_size {
        let mut id_bytes = [0u8; 32];
        id_bytes[0..8].copy_from_slice(&(i as u64).to_be_bytes());
        cache.put(Id::from_bytes(id_bytes), vec![0u8; 256]);
    }

    c.bench_function("cache_eviction", |b| {
        let mut counter = cache_size as u64;
        b.iter(|| {
            let mut id_bytes = [0u8; 32];
            id_bytes[0..8].copy_from_slice(&counter.to_be_bytes());
            counter += 1;
            cache.put(Id::from_bytes(id_bytes), vec![0u8; 256]);
        });
    });
}

fn bench_block_store_cached(c: &mut Criterion) {
    let store = BlockStore::new(1000);

    // Prepopulate
    for i in 0..1000u64 {
        let mut id_bytes = [0u8; 32];
        id_bytes[0..8].copy_from_slice(&i.to_be_bytes());
        store.put_block(Id::from_bytes(id_bytes), vec![0u8; 256]);
    }

    let test_id = {
        let mut id_bytes = [0u8; 32];
        id_bytes[0..8].copy_from_slice(&500u64.to_be_bytes());
        Id::from_bytes(id_bytes)
    };

    c.bench_function("block_store_cached", |b| {
        b.iter(|| black_box(store.get_block(&test_id)));
    });
}

fn bench_block_store_uncached(c: &mut Criterion) {
    let store = BlockStore::new(100); // Small cache

    // Add many blocks
    for i in 0..1000u64 {
        let mut id_bytes = [0u8; 32];
        id_bytes[0..8].copy_from_slice(&i.to_be_bytes());
        store.put_block(Id::from_bytes(id_bytes), vec![0u8; 256]);
    }

    // Clear cache by accessing different blocks
    for i in 900..1000u64 {
        let mut id_bytes = [0u8; 32];
        id_bytes[0..8].copy_from_slice(&i.to_be_bytes());
        store.get_block(&Id::from_bytes(id_bytes));
    }

    // Test accessing block not in cache
    let test_id = {
        let mut id_bytes = [0u8; 32];
        id_bytes[0..8].copy_from_slice(&50u64.to_be_bytes());
        Id::from_bytes(id_bytes)
    };

    c.bench_function("block_store_uncached", |b| {
        b.iter(|| black_box(store.get_block(&test_id)));
    });
}

fn bench_concurrent_db_access(c: &mut Criterion) {
    use std::sync::Arc;

    let db = Arc::new(BenchDB::new());

    // Prepopulate
    for i in 0..1000u64 {
        db.put(i.to_be_bytes().to_vec(), vec![0u8; 256]);
    }

    c.bench_function("concurrent_db_access", |b| {
        b.iter(|| {
            let db = db.clone();
            let handles: Vec<_> = (0..4)
                .map(|t| {
                    let db = db.clone();
                    std::thread::spawn(move || {
                        for i in 0..100 {
                            let key = ((t * 100 + i) as u64).to_be_bytes().to_vec();
                            black_box(db.get(&key));
                        }
                    })
                })
                .collect();

            for handle in handles {
                handle.join().unwrap();
            }
        });
    });
}

fn bench_merkle_path(c: &mut Criterion) {
    use sha2::{Digest, Sha256};

    // Simulate merkle path verification
    fn hash_pair(left: &[u8; 32], right: &[u8; 32]) -> [u8; 32] {
        let mut hasher = Sha256::new();
        hasher.update(left);
        hasher.update(right);
        hasher.finalize().into()
    }

    c.bench_function("merkle_path_verification", |b| {
        let leaf = [1u8; 32];
        let path: Vec<[u8; 32]> = (0..20).map(|i| [i as u8; 32]).collect();

        b.iter(|| {
            let mut current = leaf;
            for sibling in &path {
                current = hash_pair(&current, sibling);
            }
            black_box(current)
        });
    });
}

fn bench_state_root_computation(c: &mut Criterion) {
    use sha2::{Digest, Sha256};

    let mut group = c.benchmark_group("state_root");

    for num_leaves in [100, 1000, 10000].iter() {
        let leaves: Vec<[u8; 32]> = (0..*num_leaves)
            .map(|i| {
                let mut hasher = Sha256::new();
                hasher.update(&(i as u64).to_be_bytes());
                hasher.finalize().into()
            })
            .collect();

        group.throughput(Throughput::Elements(*num_leaves as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(num_leaves),
            &leaves,
            |b, leaves| {
                b.iter(|| {
                    // Simple merkle root computation
                    let mut level = leaves.clone();
                    while level.len() > 1 {
                        let mut next_level = Vec::with_capacity((level.len() + 1) / 2);
                        for chunk in level.chunks(2) {
                            let mut hasher = Sha256::new();
                            hasher.update(&chunk[0]);
                            if chunk.len() > 1 {
                                hasher.update(&chunk[1]);
                            } else {
                                hasher.update(&chunk[0]);
                            }
                            next_level.push(hasher.finalize().into());
                        }
                        level = next_level;
                    }
                    black_box(level.get(0).copied())
                });
            },
        );
    }

    group.finish();
}

criterion_group!(
    benches,
    bench_db_put,
    bench_db_get,
    bench_db_batch_put,
    bench_db_iterate_prefix,
    bench_cache_hit,
    bench_cache_miss,
    bench_cache_eviction,
    bench_block_store_cached,
    bench_block_store_uncached,
    bench_concurrent_db_access,
    bench_merkle_path,
    bench_state_root_computation,
);

criterion_main!(benches);
