// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{array::from_fn, num::NonZeroU64};

use bincode::Options;
use criterion::{criterion_group, criterion_main, Criterion};
use smallvec::SmallVec;
use storage::{LeafNode, Node, Path};

fn leaf(c: &mut Criterion) {
    let mut group = c.benchmark_group("leaf");
    let input = Node::Leaf(LeafNode {
        partial_path: Path(SmallVec::from_slice(&[0, 1, 2, 3, 4, 5, 6, 7, 8, 9])),
        value: SmallVec::from_slice(&[0, 1, 2, 3, 4, 5, 6, 7, 8, 9]),
    });
    let serializer = bincode::DefaultOptions::new().with_varint_encoding();
    group.bench_with_input("leaf", &input, |b, input| {
        b.iter(|| {
            serializer.serialize(input).unwrap();
        })
    });
}

fn branch(c: &mut Criterion) {
    let mut group = c.benchmark_group("branch");
    let mut input = Node::Branch(Box::new(storage::BranchNode {
        partial_path: Path(SmallVec::from_slice(&[0, 1, 2, 3, 4, 5, 6, 7, 8, 9])),
        value: Some(vec![0, 1, 2, 3, 4, 5, 6, 7, 8, 9].into_boxed_slice()),
        children: from_fn(|i| {
            if i == 0 {
                Some(storage::Child::AddressWithHash(
                    NonZeroU64::new(1).unwrap(),
                    storage::TrieHash::from([0; 32]),
                ))
            } else {
                None
            }
        }),
    }));
    let serializer = bincode::DefaultOptions::new().with_varint_encoding();
    let benchfn = |b: &mut criterion::Bencher, input: &storage::Node| {
        b.iter(|| {
            serializer.serialize(input).unwrap();
        })
    };

    group.bench_with_input("1_child+has_value", &input, benchfn);

    input.as_branch_mut().unwrap().value = None;
    group.bench_with_input("1_child", &input, benchfn);
    let child = input.as_branch().unwrap().children[0].clone();

    input.as_branch_mut().unwrap().children[1] = child.clone();
    group.bench_with_input("2_child", &input, benchfn);

    input.as_branch_mut().unwrap().children = std::array::from_fn(|_| child.clone());
    group.bench_with_input("16_child", &input, benchfn);
}

criterion_group!(serializers, leaf, branch);
criterion_main!(serializers);
