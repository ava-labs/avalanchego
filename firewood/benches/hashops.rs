// hash benchmarks; run with 'cargo bench'
use std::ops::Deref;

use bencher::{benchmark_group, benchmark_main, Bencher};
use firewood::merkle::{Hash, Merkle, HASH_SIZE};
use firewood_shale::{
    compact::CompactSpaceHeader, MemStore, MummyItem, MummyObj, ObjPtr, PlainMem,
};
use rand::{distributions::Alphanumeric, Rng, SeedableRng};

const ZERO_HASH: Hash = Hash([0u8; HASH_SIZE]);

fn bench_dehydrate(b: &mut Bencher) {
    let mut to = [1u8; HASH_SIZE];
    b.iter(|| {
        ZERO_HASH.dehydrate(&mut to);
    });
}

fn bench_hydrate(b: &mut Bencher) {
    let mut store = firewood_shale::PlainMem::new(HASH_SIZE as u64, 0u8);
    store.write(0, ZERO_HASH.deref());

    b.iter(|| {
        Hash::hydrate(0, &store).unwrap();
    });
}

fn bench_insert(b: &mut Bencher) {
    const TEST_MEM_SIZE: u64 = 20_000_000;
    let merkle_payload_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(0);

    let merkle_payload_header_ref = MummyObj::ptr_to_obj(
        &PlainMem::new(2 * firewood_shale::compact::CompactHeader::MSIZE, 9),
        merkle_payload_header,
        firewood_shale::compact::CompactHeader::MSIZE,
    )
    .unwrap();

    let store = firewood_shale::compact::CompactSpace::new(
        PlainMem::new(TEST_MEM_SIZE, 0).into(),
        PlainMem::new(TEST_MEM_SIZE, 1).into(),
        merkle_payload_header_ref,
        firewood_shale::ObjCache::new(1 << 20),
        4096,
        4096,
    )
    .unwrap();
    let mut merkle = Merkle::new(Box::new(store));
    let mut root = ObjPtr::null();
    Merkle::init_root(&mut root, merkle.get_store()).unwrap();
    let mut rng = rand::rngs::StdRng::seed_from_u64(1234);
    const KEY_LEN: usize = 4;
    b.iter(|| {
        // generate a random key
        let k = (&mut rng)
            .sample_iter(&Alphanumeric)
            .take(KEY_LEN)
            .collect::<Vec<u8>>();
        merkle.insert(k, vec![b'v'], root).unwrap();
    });
    #[cfg(trace)]
    {
        merkle.dump(root, &mut io::std::stdout().lock()).unwrap();
        println!("done\n---\n\n");
    }
}

benchmark_group!(benches, bench_dehydrate, bench_hydrate, bench_insert);
benchmark_main!(benches);
