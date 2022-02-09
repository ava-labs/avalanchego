# Avalanche API

## buf

Protobuf files are hosted at [https://buf.build/ava-labs/avalanchego](https://buf.build/ava-labs/avalanchego) and can be used as dependencies in other projects.

Protobuf linting and generation for this project is managed by [buf](https://github.com/bufbuild/buf).

Please find installation instructions on [https://docs.buf.build/installation/](https://docs.buf.build/installation/) or use the provided `Dockerfile.buf`.

Any changes made to proto files can be updated by running `protobuf_codegen.sh` located in the `scripts/` directory.

Introduction to `buf` [https://docs.buf.build/tour/introduction](https://docs.buf.build/tour/introduction)
