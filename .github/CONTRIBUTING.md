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

## Documentation guidelines

- Code should be well commented, so it is easier to read and maintain.
 Any complex sections or invariants should be documented explicitly.
- Code must be documented adhering to the official Go
  [commentary](https://go.dev/doc/effective_go#commentary) guidelines.
- Changes with user facing impact (e.g., addition or modification of flags and
 options) should be accompanied by a link to a pull request to the [avalanche-docs](https://github.com/ava-labs/avalanche-docs)
 repository. [example](https://github.com/ava-labs/avalanche-docs/pull/1119/files). 
- Changes that modify a package significantly or add new features should
 either update the existing or include a new `README.md` file in that package.

## Can I have feature X

Before you submit a feature request, please check and make sure that it isn't
possible through some other means.
