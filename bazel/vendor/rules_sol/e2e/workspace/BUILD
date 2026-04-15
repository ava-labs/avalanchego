load("@bazel_skylib//rules:select_file.bzl", "select_file")
load("@aspect_rules_sol//sol:defs.bzl", "sol_binary", "sol_imports", "sol_remappings")
load("@aspect_bazel_lib//lib:write_source_files.bzl", "write_source_files")
load("@aspect_bazel_lib//lib:testing.bzl", "assert_json_matches")

sol_binary(
    name = "direct",
    srcs = ["HelloWorld.sol"],
    ast_compact_json = True,
    deps = ["@fake_remote_repo//:echo"],
)

sol_binary(
    name = "indirect",
    srcs = [":imports"],
    ast_compact_json = True,
)

sol_imports(
    name = "imports",
    srcs = ["HelloWorld.sol"],
    remappings = {"indirect-remapping/": "propagated-by-using-sol_imports-as-src/"},
    deps = ["@fake_remote_repo//:echo"],
)

sol_remappings(
    name = "remappings",
    remappings = {"extra-prefix/": "path/to/extra/"},
    deps = [":indirect"],
)

#####
# Everything below here is a test, specific to this example (to confirm that it
# works as intended), and not typically included in regular usage of rules_sol.
#####

TARGETS = [
    "direct",
    "indirect",
]

[
    select_file(
        name = "pick_%s_ast" % t,
        srcs = t,
        subpath = "HelloWorld.sol_json.ast",
    )
    for t in TARGETS
]

assert_json_matches(
    name = "direct_matches_indirect",
    file1 = "pick_direct_ast",
    file2 = "pick_indirect_ast",
)

write_source_files(
    name = "write_golden",
    files = {
        "HelloWorld.json": "pick_direct_ast",
        "remappings.txt": ":remappings",
    },
)
