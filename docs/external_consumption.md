# Depending on AvalancheGo Modules

AvalancheGo consists of multiple Go modules that are must be consumed
together:

| Module | Import Path |
|--------|-------------|
| Main | `github.com/ava-labs/avalanchego` |
| Coreth | `github.com/ava-labs/avalanchego/graft/coreth` |
| EVM | `github.com/ava-labs/avalanchego/graft/evm` |
| Subnet-EVM | `github.com/ava-labs/avalanchego/graft/subnet-evm` |

For the rationale behind this document, see [Multi-Module Release
Strategy](design/multi_module_release.md).

## Depending on a Released Version

For tagged releases, simply use `go get`:

```bash
go get github.com/ava-labs/avalanchego@v1.15.0
```

All submodules automatically resolve to matching versions:

```bash
$ go list -m all | grep ava-labs
github.com/ava-labs/avalanchego v1.15.0
github.com/ava-labs/avalanchego/graft/coreth v1.15.0
github.com/ava-labs/avalanchego/graft/evm v1.15.0
github.com/ava-labs/avalanchego/graft/subnet-evm v1.15.0
```

## Depending on Development Tags

For pre-release testing, developers with the necessary permissions can
publish development tags (e.g., `v0.0.0-myfeature`). These can be
consumed the same way as release versions:

```bash
go get github.com/ava-labs/avalanchego@v0.0.0-myfeature
```

Development tags provide the same automatic submodule resolution as
release tags. For instructions on creating development tags, see
[Development Tags](releasing.md#development-tags).

## Depending on a Specific Commit

If a development tag is not an option, it is possible to target a
given commit by retrieving each module separately:

```bash
COMMIT=abc123def456
go get github.com/ava-labs/avalanchego@$COMMIT
go get github.com/ava-labs/avalanchego/graft/coreth@$COMMIT
go get github.com/ava-labs/avalanchego/graft/evm@$COMMIT
go get github.com/ava-labs/avalanchego/graft/subnet-evm@$COMMIT
```

If you only need the main module and don't use functionality from the
graft submodules, you only need to `go get` the main module.
