use std::env;

extern crate cbindgen;

fn main() {
    let crate_dir = env::var("CARGO_MANIFEST_DIR").unwrap();

    let config = cbindgen::Config::from_file("cbindgen.toml").expect("cbindgen.toml is present");

    cbindgen::Builder::new()
        .with_crate(crate_dir)
        // Add any additional configuration options here
        .with_config(config)
        .generate()
        .map_or_else(
            |error| match error {
                cbindgen::Error::ParseSyntaxError { .. } => {}
                e => panic!("{:?}", e),
            },
            |bindings| {
                bindings.write_to_file("firewood.h");
            },
        );
}
