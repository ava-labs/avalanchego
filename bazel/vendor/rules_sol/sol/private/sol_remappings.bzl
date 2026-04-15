"""Implementation for sol_remappings."""

load("//sol:providers.bzl", "SolRemappingsInfo", "sol_remappings_info")

_ATTRS = {
    "deps": attr.label_list(
        doc = "sol_binary, sol_imports, or other sol_remappings targets from which remappings are combined.",
        providers = [[SolRemappingsInfo]],
        mandatory = True,
    ),
    "remappings": attr.string_dict(
        doc = "Additional import remappings.",
        default = {},
    ),
}

def _write(ctx, output_file, remappings_info, lib_dir = ""):
    ctx.actions.write(
        output = output_file,
        content = "\n".join([
            "{prefix}={lib_dir}{path}".format(
                prefix = x[0],
                path = x[1],
                lib_dir = lib_dir,
            )
            for x in remappings_info.remappings.items()
        ]),
    )

def _sol_remappings_impl(ctx):
    remappings_info = sol_remappings_info(ctx, ctx.attr.remappings)

    output = ctx.actions.declare_file("remappings.txt")
    _write(ctx, output, remappings_info)

    return [
        DefaultInfo(files = depset([output])),
        remappings_info,
    ]

sol_remappings = struct(
    implementation = _sol_remappings_impl,
    attrs = _ATTRS,
    write = _write,
)
