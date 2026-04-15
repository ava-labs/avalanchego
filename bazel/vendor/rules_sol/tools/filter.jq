[
    .builds[] | {
        "key": .version,
        "value": {
            "path",
            # Hashes start with a "0x" which we strip
            "sha256": .sha256[2:]
        }
    }
] | from_entries