# A firewood golang interface

This allows calling into firewood from golang

## Building

First, build the release version (`cargo build --release`). This creates the ffi
interface file "firewood.h" as a side effect.

Then, you can run the tests in go, using `go test .`
