"""This module implements the language-specific toolchain rule.
"""

load("//sol/private:versions.bzl", "TOOL_VERSIONS")

SolInfo = provider(
    doc = "Information about how to invoke the tool executable.",
    fields = {
        "target_tool_path": "Path to the tool executable for the target platform.",
        "tool_files": """Files required in runfiles to make the tool executable available.

May be empty if the target_tool_path points to a locally installed tool binary.""",
        "solc_version": "Semantic version of the solc binary referenced by `target_tool_path`.",
    },
)

# Avoid using non-normalized paths (workspace/../other_workspace/path)
def _to_manifest_path(ctx, file):
    if file.short_path.startswith("../"):
        return "external/" + file.short_path[3:]
    else:
        return ctx.workspace_name + "/" + file.short_path

def _parse_solc_version(solc_bin_file):
    """Parses and returns a semantic version string from solc_bin_file.basename."""
    prefixes = ["solc-"]
    prefixes.extend(TOOL_VERSIONS.keys())
    prefixes.append("-v")

    solc_version = solc_bin_file.basename
    for p in prefixes:
        solc_version = solc_version.removeprefix(p)

    commit_pos = solc_version.find("+commit")
    if commit_pos == -1:
        fail("no solc commit found")

    return solc_version[:commit_pos]

def _sol_toolchain_impl(ctx):
    if ctx.attr.target_tool and ctx.attr.target_tool_path:
        fail("Can only set one of target_tool or target_tool_path but both were set.")
    if not ctx.attr.target_tool and not ctx.attr.target_tool_path:
        fail("Must set one of target_tool or target_tool_path.")

    tool_files = []
    target_tool_path = ctx.attr.target_tool_path
    solc_version = None

    if ctx.attr.target_tool:
        tool_files = ctx.attr.target_tool.files.to_list()
        target_tool_path = _to_manifest_path(ctx, tool_files[0])
        solc_version = _parse_solc_version(tool_files[0])

    # Make the $(SOLC_BIN) variable available in places like genrules.
    # See https://docs.bazel.build/versions/main/be/make-variables.html#custom_variables
    template_variables = platform_common.TemplateVariableInfo({
        "SOLC_BIN": target_tool_path,
    })
    default = DefaultInfo(
        files = depset(tool_files),
        runfiles = ctx.runfiles(files = tool_files),
    )
    solinfo = SolInfo(
        target_tool_path = target_tool_path,
        tool_files = tool_files,
        solc_version = solc_version,
    )

    # Export all the providers inside our ToolchainInfo
    # so the resolved_toolchain rule can grab and re-export them.
    toolchain_info = platform_common.ToolchainInfo(
        solinfo = solinfo,
        template_variables = template_variables,
        default = default,
    )
    return [
        default,
        toolchain_info,
        template_variables,
    ]

sol_toolchain = rule(
    implementation = _sol_toolchain_impl,
    attrs = {
        "target_tool": attr.label(
            doc = "A hermetically downloaded executable target for the target platform.",
            mandatory = False,
            allow_single_file = True,
        ),
        "target_tool_path": attr.string(
            doc = "Path to an existing executable for the target platform.",
            mandatory = False,
        ),
    },
    doc = """Defines a sol compiler/runtime toolchain.

For usage see https://docs.bazel.build/versions/main/toolchains.html#defining-toolchains.
""",
)
