# Contributing

Thank you for considering to help out with the source code! We welcome
contributions from anyone on the internet, and are grateful for even the
smallest of fixes!

If you'd like to contribute to subnet-evm, please fork, fix, commit and send a
pull request for the maintainers to review and merge into the main code base. If
you wish to submit more complex changes though, please check up with the core
devs first on [Discord](https://chat.avalabs.org) to
ensure those changes are in line with the general philosophy of the project
and/or get some early feedback which can make both your efforts much lighter as
well as our review and merge procedures quick and simple.

## Coding guidelines

Please make sure your contributions adhere to our coding and documentation
guidelines:

- Code must adhere to the official Go
  [formatting](https://go.dev/doc/effective_go#formatting) guidelines
  (i.e. uses [gofmt](https://pkg.go.dev/cmd/gofmt)).
- Pull requests need to be based on and opened against the `master` branch.
- Pull reuqests should include a detailed description
- Commits are required to be signed. See [here](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits)
  for information on signing commits.
- Commit messages should be prefixed with the package(s) they modify.
  - E.g. "eth, rpc: make trace configs optional"

### Mocks

Mocks are auto-generated using [mockgen](https://pkg.go.dev/go.uber.org/mock/mockgen) and `//go:generate` commands in the code.

- To **re-generate all mocks**, use the command below from the root of the project:

    ```sh
    go generate -run mockgen ./...
    ```

* To **add** an interface that needs a corresponding mock generated:
  * if the file `mocks_generate_test.go` exists in the package where the interface is located, either:
    * modify its `//go:generate go tool -modfile=tools/go.mod mockgen` to generate a mock for your interface (preferred); or
    * add another `//go:generate go tool -modfile=tools/go.mod mockgen` to generate a mock for your interface according to specific mock generation settings
  * if the file `mocks_generate_test.go` does not exist in the package where the interface is located, create it with content (adapt as needed):

    ```go
    // Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
    // See the file LICENSE for licensing terms.

    package mypackage

    //go:generate go tool -modfile=tools/go.mod mockgen -package=${GOPACKAGE} -destination=mocks_test.go . YourInterface
    ```

    Notes:
    1. Ideally generate all mocks to `mocks_test.go` for the package you need to use the mocks for and do not export mocks to other packages. This reduces package dependencies, reduces production code pollution and forces to have locally defined narrow interfaces.
    1. Prefer using reflect mode to generate mocks than source mode, unless you need a mock for an unexported interface, which should be rare.
- To **remove** an interface from having a corresponding mock generated:
  1. Edit the `mocks_generate_test.go` file in the directory where the interface is defined
  1. If the `//go:generate` mockgen command line:
      * generates a mock file for multiple interfaces, remove your interface from the line
      * generates a mock file only for the interface, remove the entire line. If the file is empty, remove `mocks_generate_test.go` as well.

## Tool Dependencies

This project uses `go tool` to manage development tool dependencies in `tools/go.mod`. This isolates tool dependencies from the main application dependencies and provides consistent, version-locked tools across the team.

### Managing Tools

* To **add a new tool**:
  ```sh
  go get -tool -modfile=tools/go.mod example.com/tool/cmd/toolname@version
  ```

* To **upgrade a tool**:
  ```sh
  go get -tool -modfile=tools/go.mod example.com/tool/cmd/toolname@newversion
  ```

* To **run a tool manually**:
  ```sh
  go tool -modfile=tools/go.mod toolname [args...]
  ```

Note: `ginkgo` remains in the main `go.mod` as it is used directly in e2e test code.
