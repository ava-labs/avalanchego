"""# Bazel rules for Solidity

See <https://docs.soliditylang.org>
"""

load("//sol/private:sol_binary.bzl", lib = "sol_binary")
load("//sol/private:sol_remappings.bzl", remap = "sol_remappings")
load("//sol/private:sol_imports.bzl", imports = "sol_imports")
load(":providers.bzl", "SolBinaryInfo", "SolImportsInfo", "SolRemappingsInfo")

sol_binary = rule(
    implementation = lib.implementation,
    attrs = lib.attrs,
    cfg = lib.solc.version_transition,
    doc = """sol_binary compiles Solidity source files with solc""",
    toolchains = lib.solc.toolchains,
    provides = [SolBinaryInfo, SolRemappingsInfo],
)

solc = lib.solc

sol_remappings = rule(
    implementation = remap.implementation,
    attrs = remap.attrs,
    doc = """sol_remappings combines remappings from multiple targets, and generates a Forge-compatible remappings.txt file.""",
    provides = [SolRemappingsInfo],
)

write_remappings_info = remap.write

sol_imports = rule(
    implementation = imports.implementation,
    attrs = imports.attrs,
    doc = """Collect .sol source files to be imported as library code.
    Performs no actions, so semantically equivalent to filegroup().
    """,
    provides = [SolImportsInfo, SolRemappingsInfo],
)

gather_transitive_imports = imports.gather_transitive
