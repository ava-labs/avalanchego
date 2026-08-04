# GitHub configuration

This directory contains configuration that GitHub uses for this repository.

## CI

- [CI conventions](../docs/ci.md) define the repository CI rules.
- [Workflows](./workflows/) define GitHub Actions automation.
- [Local composite actions](./actions/) contain reusable GitHub Actions steps.
- [Actionlint configuration](./actionlint.yml) configures actionlint.

Run `task lint-action` to check GitHub Actions configuration.

## Repository management

- [Code owners](./CODEOWNERS) define code-review ownership.
- [Dependabot](./dependabot.yml) configures dependency updates.
- [Issue templates](./ISSUE_TEMPLATE/) provide issue templates.
- [Pull request template](./pull_request_template.md) defines the pull request
  description template.

## Packaging

- [Packaging documentation](./packaging/README.md) explains package builds and
  release CI.
