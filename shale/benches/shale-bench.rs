use bencher::{benchmark_group, benchmark_main, Bencher};

use rand::Rng;
use shale::{compact::CompactSpaceHeader, MemStore, MummyObj, ObjPtr, PlainMem};

fn get_view(b: &mut Bencher) {
    const SIZE: u64 = 2_000_000;

    let mut m = PlainMem::new(SIZE, 0);
    let mut rng = rand::thread_rng();

    b.iter(|| {
        let len = rng.gen_range(0..26);
        let rdata = &"abcdefghijklmnopqrstuvwxyz".as_bytes()[..len];

        let offset = rng.gen_range(0..SIZE - len as u64);

        m.write(offset, &rdata);
        let view = m.get_view(offset, rdata.len().try_into().unwrap()).unwrap();
        assert_eq!(view.as_deref(), rdata);
    });
}
fn serialize(b: &mut Bencher) {
    const SIZE: u64 = 2_000_000;

    let m = PlainMem::new(SIZE, 0);

    b.iter(|| {
        let compact_header_obj: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(0x0);
        let _compact_header =
            MummyObj::ptr_to_obj(&m, compact_header_obj, shale::compact::CompactHeader::MSIZE)
                .unwrap();
    });
}
benchmark_group!(benches, get_view, serialize);
benchmark_main!(benches);
