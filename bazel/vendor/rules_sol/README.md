> [!NOTE]
> These Bazel rules were originally released at https://github.com/aspect-build/rules_sol.
> They were further developed privately by @ARR4N.
> The initial commit of this directory is the final version of the latter.

# Bazel rules for sol

## Toolchain

Solidity code can require a specific version of the compiler.

## Installation

From the release you wish to use:
<https://github.com/aspect-build/rules_sol/releases>
copy the WORKSPACE snippet into your `WORKSPACE` file.

To use a commit rather than a release, you can point at any SHA of the repo.

For example to use commit `abc123`:

1. Replace `url = "https://github.com/aspect-build/rules_sol/releases/download/v0.1.0/rules_sol-v0.1.0.tar.gz"` with a GitHub-provided source archive like `url = "https://github.com/aspect-build/rules_sol/archive/abc123.tar.gz"`
1. Replace `strip_prefix = "rules_sol-0.1.0"` with `strip_prefix = "rules_sol-abc123"`
1. Update the `sha256`. The easiest way to do this is to comment out the line, then Bazel will print a message with the correct value.

Note that GitHub source archives don't have a strong guarantee on the sha256 stability, see <https://github.blog/2023-02-21-update-on-the-future-stability-of-source-code-archives-and-hashes/>
