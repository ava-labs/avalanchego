// Copyright 2020 Parity Technologies
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0> or the MIT license
// <LICENSE-MIT or http://opensource.org/licenses/MIT>, at your
// option. This file may not be copied, modified, or distributed
// except according to those terms.

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 5 occurrences after enabling the lint."
)]
#![expect(
    clippy::indexing_slicing,
    reason = "Found 1 occurrences after enabling the lint."
)]

use criterion::{Criterion, criterion_group, criterion_main};
use ethereum_types::H256;
use firewood_triehash::trie_root;
use keccak_hasher::KeccakHasher;
use tiny_keccak::{Hasher, Keccak};
use trie_standardmap::{Alphabet, StandardMap, ValueMode};

fn keccak256(input: &[u8]) -> [u8; 32] {
    let mut keccak256 = Keccak::v256();
    let mut out = [0u8; 32];
    keccak256.update(input);
    keccak256.finalize(&mut out);
    out
}

fn random_word(alphabet: &[u8], min_count: usize, diff_count: usize, seed: &mut H256) -> Vec<u8> {
    assert!(min_count + diff_count <= 32);
    *seed = H256(keccak256(seed.as_bytes()));
    let r = min_count + (seed[31] as usize % (diff_count + 1));
    let mut ret: Vec<u8> = Vec::with_capacity(r);
    for i in 0..r {
        ret.push(alphabet[seed[i] as usize % alphabet.len()]);
    }
    ret
}

fn random_bytes(min_count: usize, diff_count: usize, seed: &mut H256) -> Vec<u8> {
    assert!(min_count + diff_count <= 32);
    *seed = H256(keccak256(seed.as_bytes()));
    let r = min_count + (seed[31] as usize % (diff_count + 1));
    seed[0..r].to_vec()
}

fn random_value(seed: &mut H256) -> Vec<u8> {
    *seed = H256(keccak256(seed.as_bytes()));
    match seed[0] % 2 {
        1 => vec![seed[31]; 1],
        _ => seed.as_bytes().to_vec(),
    }
}

fn bench_insertions(c: &mut Criterion) {
    c.bench_function("32_mir_1k", |b| {
        let st = StandardMap {
            alphabet: Alphabet::All,
            min_key: 32,
            journal_key: 0,
            value_mode: ValueMode::Mirror,
            count: 1000,
        };
        let d = st.make();
        b.iter(|| trie_root::<KeccakHasher, _, _, _>(d.clone()));
    });

    c.bench_function("32_ran_1k", |b| {
        let st = StandardMap {
            alphabet: Alphabet::All,
            min_key: 32,
            journal_key: 0,
            value_mode: ValueMode::Random,
            count: 1000,
        };
        let d = st.make();
        b.iter(|| trie_root::<KeccakHasher, _, _, _>(d.clone()));
    });

    c.bench_function("six_high", |b| {
        let mut d: Vec<(Vec<u8>, Vec<u8>)> = Vec::new();
        let mut seed = H256::default();
        for _ in 0..1000 {
            let k = random_bytes(6, 0, &mut seed);
            let v = random_value(&mut seed);
            d.push((k, v));
        }
        b.iter(|| trie_root::<KeccakHasher, _, _, _>(d.clone()));
    });

    c.bench_function("six_mid", |b| {
        let alphabet = b"@QWERTYUIOPASDFGHJKLZXCVBNM[/]^_";
        let mut d: Vec<(Vec<u8>, Vec<u8>)> = Vec::new();
        let mut seed = H256::default();
        for _ in 0..1000 {
            let k = random_word(alphabet, 6, 0, &mut seed);
            let v = random_value(&mut seed);
            d.push((k, v));
        }
        b.iter(|| trie_root::<KeccakHasher, _, _, _>(d.clone()));
    });

    c.bench_function("random_mid", |b| {
        let alphabet = b"@QWERTYUIOPASDFGHJKLZXCVBNM[/]^_";
        let mut d: Vec<(Vec<u8>, Vec<u8>)> = Vec::new();
        let mut seed = H256::default();
        for _ in 0..1000 {
            let k = random_word(alphabet, 1, 5, &mut seed);
            let v = random_value(&mut seed);
            d.push((k, v));
        }
        b.iter(|| trie_root::<KeccakHasher, _, _, _>(d.clone()));
    });

    c.bench_function("six_low", |b| {
        let alphabet = b"abcdef";
        let mut d: Vec<(Vec<u8>, Vec<u8>)> = Vec::new();
        let mut seed = H256::default();
        for _ in 0..1000 {
            let k = random_word(alphabet, 6, 0, &mut seed);
            let v = random_value(&mut seed);
            d.push((k, v));
        }
        b.iter(|| trie_root::<KeccakHasher, _, _, _>(d.clone()));
    });
}

criterion_group!(benches, bench_insertions);
criterion_main!(benches);
