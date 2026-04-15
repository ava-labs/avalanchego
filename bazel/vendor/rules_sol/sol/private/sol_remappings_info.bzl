"""Shared functionality used by one or more rules."""

SolRemappingsInfo = provider(
    doc = """Stores a dictionary of solc remappings.
    
    Allows for piping of remappings through a dependency tree of targets. Rules
    that accept a "remappings" attribute and/or dependencies that provide
    SolRemappingsInfo SHOULD propagate their union via a SolRemappingsInfo.
    """,
    fields = {
        "remappings": "dictionary of import remappings to propagate to solc",
    },
)

def _transitive_remappings(attr, remappings = {}):
    for a in attr:
        if SolRemappingsInfo in a:
            for prefix, target in a[SolRemappingsInfo].remappings.items():
                if prefix in remappings and remappings[prefix] != target:
                    fail("Conflicting remappings prefix %s" % prefix)
                remappings[prefix] = target
    return remappings

def sol_remappings_info(ctx, extra_remappings = {}):
    """Construct a SolRemappingsInfo.

    Fails if duplicate remapping prefixes are found with different targets.

    Args:
        ctx: Context object from the implementation constructing a SolRemappingsInfo.
        extra_remappings: Additional remappings to be added to those found in ctx.deps.

    Returns:
        SolRemappingsInfo transitively combining all SolRemappingsInfo in ctx.deps plus extra_remappings.
    """

    remappings = {k: v for (k, v) in extra_remappings.items()}
    for attr in ["srcs", "deps"]:
        if hasattr(ctx.attr, attr):
            remappings = _transitive_remappings(getattr(ctx.attr, attr), remappings)

    return SolRemappingsInfo(
        remappings = remappings,
    )
