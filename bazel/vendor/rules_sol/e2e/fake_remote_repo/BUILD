load("@aspect_rules_sol//sol:defs.bzl", "sol_imports")
load("@bazel_skylib//rules:write_file.bzl", "write_file")

REMAP_TARGET = "./external/fake_remote_repo/"

sol_imports(
    name = "echo",
    srcs = ["Echo.sol"],
    remappings = {
        "remapped-fake-remote-repo/": REMAP_TARGET,
        # Although this alternate isn't used for imports, it's included to
        # demonstrate complete generation of remappings.txt by the sol_binary()
        # that depends on this sol_imports().
        "alternate-remapped-fake-remote-repo/": REMAP_TARGET,
    },
    visibility = ["//visibility:public"],
)

# The common CI workflow for all workspaces in this repo calls `bazel test`,
# which fails if no tests are present. Instead of disabling said call and
# risking future tests added here not being performed, we have a no-op that
# should be removed if real tests are ever added.
sh_test(
    name = "noop_test",
    srcs = ["noop.sh"],
)

write_file(
    name = "empty",
    out = "noop.sh",
    content = [],
)
