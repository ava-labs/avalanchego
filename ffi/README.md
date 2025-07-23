# Firewood Golang FFI

The FFI package provides a golang FFI layer for Firewood.

## Building Firewood Golang FFI

The Golang FFI layer uses a CGO directive to locate a C-API compatible binary built from Firewood. Firewood supports both seamless local development and a single-step compilation process for Go projects that depend or transitively depend on Firewood.

To do this, [firewood.go](./firewood.go) includes CGO directives to include multiple search paths for the Firewood binary in the local `target/` build directory and `ffi/libs`. For the latter, [attach-static-libs](../.github/workflows/attach-static-libs.yaml) GitHub Action pushes an FFI package for `ethhash` with static libraries attached for the following supported architectures:

- x86_64-unknown-linux-gnu
- aarch64-unknown-linux-gnu
- aarch64-apple-darwin
- x86_64-apple-darwin

to a separate repo [firewood-go-ethhash](https://github.com/ava-labs/firewood-go-ethhash) (to avoid including binaries in the Firewood repo).

### Local Development

[firewood.go](./firewood.go) includes CGO directives to include builds in the `target/` directory.

Firewood prioritizes builds in the following order:

1. maxperf
2. release
3. debug

To use and test the Firewood FFI locally, you can run:

```bash
cargo build --profile maxperf
cd ffi
go test
```

To use a local build of Firewood for a project that depends on Firewood, you must redirect the `go.mod` to use the local version of Firewood FFI, for example:

```bash
go mod edit -replace github.com/ava-labs/firewood-go-ethhash/ffi=/path/to/firewood/ffi
go mod tidy
```

### Production Development Flow

Firewood pushes the FFI source code and attached static libraries to [firewood-go-ethhash](https://github.com/ava-labs/firewood-go-ethhash) via [attach-static-libs](../.github/workflows/attach-static-libs.yaml).

This enables consumers to utilize it directly without forcing them to compile Firewood locally. Go programs running on supported architectures can utilize `firewood-go-ethhash/ffi` just like any other dependency.

To trigger this build, [attach-static-libs](../.github/workflows/attach-static-libs.yaml) supports triggers for both manual GitHub Actions and tags, so you can create a mirror branch/tag on [firewood-go-ethhash](https://github.com/ava-labs/firewood-go-ethhash) by either trigger a manual GitHub Action and selecting your branch or pushing a tag to Firewood.

### Hash Mode

Firewood implemented its own optimized merkle trie structure. To support Ethereum Merkle Trie hash compatibility, it also provides a feature flag `ethhash`.

This is an optional feature (disabled by default). To enable it for a local build, compile with:

```sh
cargo build -p firewood-ffi --features ethhash
```

To support development in [Coreth](https://github.com/ava-labs/coreth), Firewood pushes static libraries for Ethereum-compatible hashing to [firewood-go-ethhash](https://github.com/ava-labs/firewood-go-ethhash) with `ethhash` enabled by default. To use Firewood's native hashing structure, you must still build the static library separately.

## Configuration

### Database Config

A `Config` should be provided when creating the database. A default config is provided at `ffi.DefaultConfig()`:

```go
&Config{
    NodeCacheEntries:     1000000,
    FreeListCacheEntries: 40000,
    Revisions:            100,
    ReadCacheStrategy:    OnlyCacheWrites,
}
```

If no config is provided (`config == nil`), the default config is used. A description of all available values, and the default if not set, is available below.

#### `Truncate` - `bool`

If set to `true`, an empty database will be created, overriding any existing file. Otherwise, if a file exists, that file will be loaded to create the database. In either case, if the file doesn't exist, it will be created.

*Default*: `false`

#### `Revisions` - `uint`

Indicates the number of committed roots accessible before the diff layer is compressed. Must be explicitly set if the config is specified to at least 2.

#### `ReadCacheStrategy` - `uint`

Should be one of `OnlyCacheWrites`, `CacheBranchReads`, or `CacheAllReads`. In the latter two cases, writes are still cached.

*Default*: `OnlyCacheWrites`

#### `NodeCacheEntries`- `uint`

The number of nodes in the database that are stored in cache. Must be explicitly set if the config is supplied.

#### `FreeListCacheEntries` - `uint`

The number of entries in the free list (see [Firewood Overview](../README.md)). Must be explicitly set if the config is supplied.

### Metrics

By default, metrics are not enabled in Firewood's FFI. However, if compiled with this option, they can be recorded by a call to `StartMetrics()` or `StartMetricsWithExporter(port)`. One of these may be called exactly once, since it starts the metrics globally on the process.

To use these metrics, you can:

- Listen on the port specified, if you started the metrics with the exporter.
- Call `GatherMetrics()`, which returns an easily parsable string containing all metrics.
- Create the Prometheus gatherer, and call `Gather`. This can easily be integrated into other applications which already use prometehus. Example usage is below:

```go
gatherer := ffi.Gatherer{}
metrics, err := gatherer.Gather()
```

### Logs

Logs are configured globally on the process, and not enabled by default. They can be enabled using the `StartLogs(config)` function. Firewood must be built with the `logger` feature for this function to work. This should be called before opening the database.

#### `Path` - `string`

The path to the file where the logs will be written.

*Default*: `{TEMP}/firewood-log.txt`, where `{TEMP}` is the platform's temporary directory. For Unix-based OSes, this is typically `/tmp`.

#### `FilterLevel` - `string`

One of `trace`, `debug`, `info`, `warn` or `error`.

*Default*: `info`

## Development

Iterative building is unintuitive for the ffi and some common sources of confusion are listed below.

### CGO Regeneration

As you edit any Rust code and save the file in VS Code, the `firewood.h` file is automatically updated with edited function and struct definitions. However, the Go linter will not recognize these changes until you manually regenerate the cgo wrappers. To do this, you can run `go tool cgo firewood.go`. Alternatively, in VS Code, right above the `import "C"` definition, you can click on the small letters saying "regenerate CGO definitions". This will allow the linter to use the altered definitions.

Because the C header file is autogenerated from the Rust code, the naming matches exactly (due to the `no_mangle` macro). However, the C definitions imported in Go do not match exactly, and are prefixed with `struct_`. Function naming is the same as the header file. These names are generated by the `go tool cgo` command above.

### Testing

Although the VS Code testing feature does work, there are some quirks in ensuring proper building. The Rust code must be compiled separated, and sometimes the `go test` command continues to use a cached result. Whenever testing after making changes to the Rust/C builds, the cache should be cleared if results don't seem correct. Do not compile with `--features ethhash`, as some tests will fail.

To ensure there are no memory leaks, the easiest way is to use your preferred CLI tool (e.g. `valgrind` for Linux, `leaks` for macOS) and compile the tests into a binary. You must not compile a release binary to ensure all memory can be managed. An example flow is given below.

```sh
cd ffi
cargo build # use debug
go test -a -c -o binary_file # ignore cache
leaks --nostacks --atExit -- ./binary_file
```
