# Test Network Fixture

This package contains configuration and interfaces that are
independent of a given orchestration mechanism
(e.g. [local](local/README.md)). The intent is to enable tests to be
written against the interfaces defined in this package and for
implementation-specific details of test network orchestration to be
limited to test setup and teardown.
