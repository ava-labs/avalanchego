// hash benchmarks; run with 'cargo bench'
use std::ops::Deref;

use bencher::{benchmark_group, benchmark_main, Bencher};
use firewood::merkle::{Merkle, TrieHash, TRIE_HASH_LEN};
use firewood_shale::{
    cached::PlainMem, disk_address::DiskAddress, CachedStore, Storable, StoredView,
};
use rand::{distributions::Alphanumeric, Rng, SeedableRng};

const ZERO_HASH: TrieHash = TrieHash([0u8; TRIE_HASH_LEN]);

fn bench_dehydrate(b: &mut Bencher) {
    let mut to = [1u8; TRIE_HASH_LEN];
    b.iter(|| {
        ZERO_HASH.dehydrate(&mut to).unwrap();
    });
}

fn bench_hydrate(b: &mut Bencher) {
    let mut store = PlainMem::new(TRIE_HASH_LEN as u64, 0u8);
    store.write(0, ZERO_HASH.deref());

    b.iter(|| {
        TrieHash::hydrate(0, &store).unwrap();
    });
}

fn bench_insert(b: &mut Bencher) {
    const TEST_MEM_SIZE: u64 = 20_000_000;
    let merkle_payload_header = DiskAddress::null();

    let merkle_payload_header_ref = StoredView::ptr_to_obj(
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
    let root = merkle.init_root().unwrap();

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
