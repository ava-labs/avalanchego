# Demo reproducible builds with Bazel

[Bazel](https://bazel.build) is a build system that, amongst other things, provides the ability to reproduce builds to bit-level identity. This extends beyond binaries to also produce hash-equivalent container images.

This branch is a demo of building the `avalanchego` binary with Bazel. The intent is that anyone can clone the repo and run each of the `$`-prefixed commands:

```shell
$ brew install bazelisk # Bazel version manager

$ bazel build //main # Analogous to `go build`
...
Target //main:main up-to-date:
  bazel-bin/main/main_/main
...

$ sha256sum "$(bazel info bazel-bin)/main/main_/main"
c48e93120650c4cdb93b54a0dcef50b35d6159bd7bf2aa14da6ec76956308e38 ...
```

The vast majority of new files (all the non-root `BUILD.bazel`) are auto-generated so are of little interest for the demo. The most important files are:

1. `MODULE.bazel`: primary configuration of the repository;
2. `BUILD.bazel` (root directory): adds `gazelle` support for the auto-generation; and
3. `bazel/*.patch`: one-off BUILD patches to help `gazelle` with cgo edge cases.

## In parallel with standard Go workflow

Note that:

1. This doesn't force repo consumers to themselves use Bazel.
2. Developer experience is largely unchanged as they too use a regular Go workflow if choosing to do so.

### Standardised Go installation

The Go toolchain is available through Bazel:
```shell
$ bazel run @rules_go//go version
...
go version go1.23.6 darwin/arm64
```

and an `.envrc` file can be used to alias `go` to `bazel run @rules_go//go`.

### Building container images

Bazel has extensive support for wrapping Go-binary targets in container images, as shown in
[this example](https://github.com/aspect-build/bazel-examples/tree/main/oci_go_image). Images can be built and pushed with a single `bazel run //path/to:push_image` command.

### Out of scope

This is an MVP solely to build `./main`. Other Bazel features (e.g. shared build caches, polyglot support and code generation) are beyond the scope of the demo.