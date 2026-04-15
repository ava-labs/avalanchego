"Implementation for sol_imports rule"

load("@aspect_rules_js//js:providers.bzl", "JsInfo", "js_info")
load("//sol:providers.bzl", "SolImportsInfo", "sol_remappings_info")
load("@aspect_rules_js//js:libs.bzl", "js_lib_helpers")

_ATTRS = {
    "srcs": attr.label_list(
        allow_files = [".sol"],
        doc = "Solidity source files",
    ),
    "deps": attr.label_list(
        doc = "Each dependency should either be more sol_imports, or npm packages for 3p dependencies",
        providers = [[SolImportsInfo], [JsInfo]],
    ),
    "remappings": attr.string_dict(
        doc = """Contribute to import mappings.
        
        See https://docs.soliditylang.org/en/latest/path-resolution.html?highlight=remappings#import-remapping
        """,
        default = {},
    ),
}

def _gather_transitive_imports(ctx):
    return [
        d[SolImportsInfo].transitive_sources
        for d in (ctx.attr.srcs + ctx.attr.deps)
        if SolImportsInfo in d
    ]

def _sol_imports_impl(ctx):
    npm_linked_packages = js_lib_helpers.gather_npm_linked_packages(
        srcs = ctx.attr.srcs,
        deps = ctx.attr.deps,
    )

    return [
        DefaultInfo(
            files = depset(ctx.files.srcs),
        ),
        SolImportsInfo(
            direct_sources = ctx.files.srcs,
            transitive_sources = depset(
                ctx.files.srcs,
                transitive = _gather_transitive_imports(ctx),
            ),
        ),
        sol_remappings_info(ctx, ctx.attr.remappings),
        js_info(
            npm_linked_packages = npm_linked_packages.direct,
            npm_linked_package_files = npm_linked_packages.direct_files,
            transitive_npm_linked_package_files = npm_linked_packages.transitive_files,
            transitive_npm_linked_packages = npm_linked_packages.transitive,
        ),
    ]

sol_imports = struct(
    implementation = _sol_imports_impl,
    attrs = _ATTRS,
    gather_transitive = _gather_transitive_imports,
)
