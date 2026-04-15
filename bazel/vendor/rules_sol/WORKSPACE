# Declare the local Bazel workspace.
workspace(
    # If your ruleset is "official"
    # (i.e. is in the bazelbuild GitHub org)
    # then this should just be named "rules_sol"
    # see https://docs.bazel.build/versions/main/skylark/deploying.html#workspace
    name = "aspect_rules_sol",
)

load(":internal_deps.bzl", "rules_sol_internal_deps")

# Fetch deps needed only locally for development
rules_sol_internal_deps()

load("//sol:repositories.bzl", "LATEST_VERSION", "rules_sol_dependencies", "sol_register_toolchains")

# Fetch dependencies which users need as well
rules_sol_dependencies()

# Demonstrate that we can have multiple versions of solc available to Bazel rules
[
    sol_register_toolchains(
        name = "solc_" + v.replace(".", "_"),
        sol_version = v,
    )
    for v in [
        LATEST_VERSION,
        # If changing these, also change /private/tests/BUILD.bazel as the versions are used in explicit tests of
        # compiler selection by sol_binary.solc_version.
        "0.8.9",
        "0.7.6",
        # Minimum version supporting the --no-cbor-metadata flag.
        "0.8.18",
    ]
]

load("@aspect_rules_js//js:repositories.bzl", "rules_js_dependencies")

rules_js_dependencies()

load("@rules_nodejs//nodejs:repositories.bzl", "DEFAULT_NODE_VERSION", "nodejs_register_toolchains")

nodejs_register_toolchains(
    name = "nodejs",
    node_version = DEFAULT_NODE_VERSION,
)

load("@aspect_rules_js//npm:npm_import.bzl", "npm_translate_lock")

npm_translate_lock(
    name = "npm",
    pnpm_lock = "//examples/npm_deps:pnpm-lock.yaml",
    verify_node_modules_ignored = "//:.bazelignore",
)

load("@npm//:repositories.bzl", "npm_repositories")

npm_repositories()

# For running our own unit tests
load("@bazel_skylib//:workspace.bzl", "bazel_skylib_workspace")

bazel_skylib_workspace()

load("@aspect_bazel_lib//lib:repositories.bzl", "register_jq_toolchains")

register_jq_toolchains()

############################################
# Gazelle, for generating bzl_library targets
load("@io_bazel_rules_go//go:deps.bzl", "go_register_toolchains", "go_rules_dependencies")
load("@bazel_gazelle//:deps.bzl", "gazelle_dependencies")

go_rules_dependencies()

go_register_toolchains(version = "1.17.2")

gazelle_dependencies()
