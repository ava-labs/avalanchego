# tmpnet (temporary network fixture)

This package contains configuration and interfaces that are
independent of a given orchestration mechanism
(e.g. [local](local/README.md)). The intent is to enable tests to be
written against the interfaces defined in this package and for
implementation-specific details of test network orchestration to be
limited to test setup and teardown.

## What's in a name?

The name of this package was originally `testnet` and its cli was
`testnetctl`. This name was chosen in ignorance that `testnet`
commonly refers to a persistent blockchain network used for testing.

To avoid confusion, the name was changed to `tmpnet` and its cli
`tmpnetctl`. `tmpnet` is short for `temporary network` since the
networks it deploys are likely to live for a limited duration in
support of the development and testing of avalanchego and its related
repositories.
