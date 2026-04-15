"""Implementation details for sol_binary.

TODO:
- use `--optimize` if compilation_mode=opt
- make it silent on success
"""

load("@bazel_skylib//lib:paths.bzl", "paths")
load("@bazel_skylib//lib:versions.bzl", "versions")
load("@aspect_rules_js//js:providers.bzl", "JsInfo")
load("@aspect_rules_js//js:libs.bzl", "js_lib_helpers")
load("//sol:providers.bzl", "SolBinaryInfo", "SolImportsInfo", "SolRemappingsInfo", "sol_remappings_info")
load("//sol/private:sol_imports.bzl", "sol_imports")

_OUTPUT_COMPONENTS = ["abi", "asm", "ast", "bin", "bin-runtime", "devdoc", "function-debug", "function-debug-runtime", "generated-sources", "generated-sources-runtime", "hashes", "metadata", "opcodes", "srcmap", "srcmap-runtime", "storage-layout", "userdoc"]
_ATTRS = {
    "srcs": attr.label_list(
        doc = "Solidity source files",
        mandatory = True,
        allow_files = [".sol"],
        providers = [[SolImportsInfo, SolRemappingsInfo]],
    ),
    "args": attr.string_list(
        doc = "Additional command-line arguments to solc. Run solc --help for a listing.",
    ),
    "deps": attr.label_list(
        doc = "Solidity libraries, either first-party sol_imports, or third-party distributed as packages on npm",
        providers = [[SolImportsInfo, SolRemappingsInfo], [JsInfo]],
    ),
    "bin": attr.bool(
        doc = "Whether to emit binary of the contracts in hex.",
    ),
    "ast_compact_json": attr.bool(
        doc = "Whether to emit AST of all source files in a compact JSON format.",
    ),
    "combined_json": attr.string_list(
        doc = """Output a single json document containing the specified information.""",
        # Thanks bazel... https://github.com/bazelbuild/bazel/issues/6638
        # allowed values can't be specified here
        default = ["abi", "bin", "hashes"],
    ),
    "solc_version": attr.string(),
    "_allowlist_function_transition": attr.label(
        default = "@bazel_tools//tools/allowlists/function_transition_allowlist",
    ),
    "optimize": attr.bool(
        doc = """Set the solc --optimize flag.

        See https://docs.soliditylang.org/en/latest/using-the-compiler.html#optimizer-options
        """,
    ),
    "optimize_runs": attr.int(
        doc = """Set the solc --optimize-runs flag. In keeping with solc behaviour, this has no effect unless optimize=True.
        
        See https://docs.soliditylang.org/en/latest/using-the-compiler.html#optimizer-options
        """,
        default = 200,  # same as solc
    ),
    "no_cbor_metadata": attr.bool(
        doc = """Set the solc --no-cbor-metadata flag.

If false, compiled bytecode may not be deterministic due to appended metadata. This can change due to input that has no effect on compiled output; e.g. remappings, variable names, and comments.""",
        default = True,
    ),
}

def _calculate_outs(ctx):
    """Predict what files the solc compiler will emit.

    Returns: a tuple of (a) a list of the predicted files; and (b) the combined.json
    file if it is to be emitted, otherwise None. If (b) is not None then it is the
    same file included in the list. All files are in a sub-directory named the same
    as ctx.attr.name.
    """
    result = []
    prefix = ctx.attr.name + "/"
    for src in ctx.files.srcs:
        relative_src = paths.relativize(src.short_path, ctx.label.package)
        if ctx.attr.bin:
            result.append(ctx.actions.declare_file(prefix + paths.replace_extension(relative_src, ".bin")))
        if ctx.attr.ast_compact_json:
            result.append(ctx.actions.declare_file(prefix + relative_src + "_json.ast"))

    combined_json = None
    if len(ctx.attr.combined_json):
        combined_json = ctx.actions.declare_file(prefix + "combined.json")
        result.append(combined_json)

    return (result, combined_json)

def _run_solc(ctx):
    "Generate action(s) to run the compiler"
    solinfo = ctx.toolchains["@aspect_rules_sol//sol:toolchain_type"].solinfo
    args = ctx.actions.args()

    for arg in ctx.attr.args:
        if arg in ["--optimize", "--optimize_runs", "--no-cbor-metadata"]:
            fail("{} in args list; use the sol_binary attribute instead", arg)

    # User-provided arguments first, so we can override them
    args.add_all(ctx.attr.args)

    args.add_all([s.path for s in ctx.files.srcs])

    # TODO: is this the right value? maybe it ought to be the package directory?
    args.add_all(["--base-path", "."])

    args.add("--output-dir")
    args.add_joined([ctx.bin_dir.path, ctx.label.package, ctx.attr.name], join_with = "/")

    root_packages = []
    for dep in ctx.attr.deps:
        if JsInfo in dep:
            for pkg in dep[JsInfo].transitive_npm_linked_packages.to_list():
                # Where the node_modules were installed
                root_packages.append(pkg.store_info.root_package)

    remappings_info = sol_remappings_info(ctx)
    for (prefix, target) in remappings_info.remappings.items():
        args.add_joined([prefix, target], join_with = "=")

    if len(root_packages):
        args.add("--include-path")
        args.add_joined(
            root_packages,
            format_each = ctx.bin_dir.path + "/%s/node_modules",
            join_with = ",",
            uniquify = True,
        )

    if ctx.attr.bin:
        args.add("--bin")
    if ctx.attr.ast_compact_json:
        args.add("--ast-compact-json")
    for v in ctx.attr.combined_json:
        if v not in _OUTPUT_COMPONENTS:
            fail("Illegal output component {}, must be one of {}".format(v, _OUTPUT_COMPONENTS))
    if len(ctx.attr.combined_json):
        args.add("--combined-json")
        args.add_joined(ctx.attr.combined_json, join_with = ",")
    if ctx.attr.optimize:
        args.add_all(["--optimize", "--optimize-runs", ctx.attr.optimize_runs])

    if ctx.attr.no_cbor_metadata:
        if not versions.is_at_least("0.8.18", solinfo.solc_version):
            fail("solc version %s doesn't support --no-cbor-metadata" % solinfo.solc_version)
        args.add("--no-cbor-metadata")

    (outputs, combined_json) = _calculate_outs(ctx)
    if not len(outputs):
        fail("No outputs were requested. This is illegal under Bazel, as actions are only run to produce output files.")

    npm_deps = js_lib_helpers.gather_files_from_js_providers(ctx.attr.deps, include_transitive_sources = True, include_declarations = False, include_npm_linked_packages = True)

    # solc will follow symlinks out of the sandbox, then insist that the execroot path is allowed.
    #
    # This seems to be an understood limitation, for example in https://github.com/ethereum/solidity/issues/11410:
    # > NOTE: --allowed-directories becomes almost redundant after these changes. There are now only two cases where it's needed:
    # > When a file is a symlink that leads to a file outside of base path or include directories.
    # > The directory containing the symlink target must be in --allowed-directories for this to be allowed.
    #
    # Effectively disable this security feature - Bazel's sandbox ensures reproducibility
    # Anyhow, as very few compilers do such a thing, the solc layer isn't the right place to solve.
    args.add_all(["--allow-paths", "/"])

    solc_inputs = depset(ctx.files.srcs, transitive = sol_imports.gather_transitive(ctx) + [npm_deps])
    ctx.actions.run(
        executable = solinfo.target_tool_path,
        arguments = [args],
        inputs = solc_inputs,
        outputs = outputs,
        tools = solinfo.tool_files,
        mnemonic = "Solc",
        progress_message = "solc compile " + outputs[0].short_path,
    )

    return [
        DefaultInfo(files = depset(outputs)),
        SolBinaryInfo(
            solc_version = solinfo.solc_version,
            solc_bin = solinfo.tool_files[0].basename,
            combined_json = combined_json,
            transitive_sources = solc_inputs,
        ),
        remappings_info,
    ]

def _sol_binary_impl(ctx):
    return _run_solc(ctx)

def _solc_version_transition_impl(settings, attr):
    if attr.solc_version:
        return {
            "//sol/private:solc_version": attr.solc_version,
        }
    return {}

solc_version_transition = transition(
    implementation = _solc_version_transition_impl,
    inputs = [],
    outputs = ["//sol/private:solc_version"],
)

sol_binary = struct(
    implementation = _sol_binary_impl,
    attrs = _ATTRS,
    solc = struct(
        toolchains = ["@aspect_rules_sol//sol:toolchain_type"],
        version_transition = solc_version_transition,
    ),
)
