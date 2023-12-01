// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![allow(clippy::indexing_slicing)]
#[cfg(test)]
mod common;

use std::collections::HashMap;
use std::rc::Rc;

fn multi_point_failure(sims: &[common::PaintingSim]) {
    fn track_recursion(sims: &[common::PaintingSim], state: &common::WalStoreEmulState, f: usize) {
        let sim = &sims[0];
        // save the current state and start from there
        let mut state = state.clone();
        let mut state0 = state.clone();
        let nticks = sim.get_nticks(&mut state0);
        println!("fail = {f}, nticks = {nticks}");

        for pos in 0..nticks {
            println!("fail = {f}, pos = {pos}");
            let mut canvas = sim.new_canvas();
            let mut ops: Vec<common::PaintStrokes> = Vec::new();
            let mut ringid_map = HashMap::new();
            let fgen = common::SingleFailGen::new(pos);

            let sim_result = sim.run(
                &mut state,
                &mut canvas,
                sim.get_walloader(),
                &mut ops,
                &mut ringid_map,
                Rc::new(fgen),
            );

            if sim_result.is_ok() {
                return;
            }

            if sims.len() > 1 {
                track_recursion(&sims[1..], &state, f + 1)
            } else {
                assert!(sim.check(
                    &mut state,
                    &mut canvas,
                    sim.get_walloader(),
                    &ops,
                    &ringid_map,
                ))
            }
        }
    }

    track_recursion(sims, &common::WalStoreEmulState::new(), 1);
}

#[test]
fn short_single_point_failure() {
    let sim = common::PaintingSim {
        block_nbit: 5,
        file_nbit: 6,
        file_cache: 1000,
        n: 10,
        m: 10,
        k: 10,
        csize: 100,
        stroke_max_len: 10,
        stroke_max_col: 256,
        stroke_max_n: 2,
        seed: 0,
    };
    multi_point_failure(&[sim]);
}

#[test]
#[ignore]
fn single_point_failure1() {
    let sim = common::PaintingSim {
        block_nbit: 5,
        file_nbit: 6,
        file_cache: 1000,
        n: 100,
        m: 10,
        k: 1000,
        csize: 1000,
        stroke_max_len: 10,
        stroke_max_col: 256,
        stroke_max_n: 5,
        seed: 0,
    };
    multi_point_failure(&[sim]);
}

#[test]
#[ignore]
fn two_failures() {
    let sims = [
        common::PaintingSim {
            block_nbit: 5,
            file_nbit: 6,
            file_cache: 1000,
            n: 10,
            m: 5,
            k: 100,
            csize: 1000,
            stroke_max_len: 10,
            stroke_max_col: 256,
            stroke_max_n: 3,
            seed: 0,
        },
        common::PaintingSim {
            block_nbit: 5,
            file_nbit: 6,
            file_cache: 1000,
            n: 10,
            m: 5,
            k: 100,
            csize: 1000,
            stroke_max_len: 10,
            stroke_max_col: 256,
            stroke_max_n: 3,
            seed: 0,
        },
    ];
    multi_point_failure(&sims);
}
