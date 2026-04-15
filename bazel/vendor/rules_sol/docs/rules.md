<!-- Generated with Stardoc: http://skydoc.bazel.build -->

# Bazel rules for Solidity

See <https://docs.soliditylang.org>


<a id="sol_binary"></a>

## sol_binary

<pre>
sol_binary(<a href="#sol_binary-name">name</a>, <a href="#sol_binary-args">args</a>, <a href="#sol_binary-ast_compact_json">ast_compact_json</a>, <a href="#sol_binary-bin">bin</a>, <a href="#sol_binary-combined_json">combined_json</a>, <a href="#sol_binary-deps">deps</a>, <a href="#sol_binary-no_cbor_metadata">no_cbor_metadata</a>, <a href="#sol_binary-optimize">optimize</a>,
           <a href="#sol_binary-optimize_runs">optimize_runs</a>, <a href="#sol_binary-solc_version">solc_version</a>, <a href="#sol_binary-srcs">srcs</a>)
</pre>

sol_binary compiles Solidity source files with solc

**ATTRIBUTES**


| Name  | Description | Type | Mandatory | Default |
| :------------- | :------------- | :------------- | :------------- | :------------- |
| <a id="sol_binary-name"></a>name |  A unique name for this target.   | <a href="https://bazel.build/concepts/labels#target-names">Name</a> | required |  |
| <a id="sol_binary-args"></a>args |  Additional command-line arguments to solc. Run solc --help for a listing.   | List of strings | optional | [] |
| <a id="sol_binary-ast_compact_json"></a>ast_compact_json |  Whether to emit AST of all source files in a compact JSON format.   | Boolean | optional | False |
| <a id="sol_binary-bin"></a>bin |  Whether to emit binary of the contracts in hex.   | Boolean | optional | False |
| <a id="sol_binary-combined_json"></a>combined_json |  Output a single json document containing the specified information.   | List of strings | optional | ["abi", "bin", "hashes"] |
| <a id="sol_binary-deps"></a>deps |  Solidity libraries, either first-party sol_imports, or third-party distributed as packages on npm   | <a href="https://bazel.build/concepts/labels">List of labels</a> | optional | [] |
| <a id="sol_binary-no_cbor_metadata"></a>no_cbor_metadata |  Set the solc --no-cbor-metadata flag.<br><br>If false, compiled bytecode may not be deterministic due to appended metadata. This can change due to input that has no effect on compiled output; e.g. remappings, variable names, and comments.   | Boolean | optional | True |
| <a id="sol_binary-optimize"></a>optimize |  Set the solc --optimize flag.<br><br>        See https://docs.soliditylang.org/en/latest/using-the-compiler.html#optimizer-options   | Boolean | optional | False |
| <a id="sol_binary-optimize_runs"></a>optimize_runs |  Set the solc --optimize-runs flag. In keeping with solc behaviour, this has no effect unless optimize=True.<br><br>        See https://docs.soliditylang.org/en/latest/using-the-compiler.html#optimizer-options   | Integer | optional | 200 |
| <a id="sol_binary-solc_version"></a>solc_version |  -   | String | optional | "" |
| <a id="sol_binary-srcs"></a>srcs |  Solidity source files   | <a href="https://bazel.build/concepts/labels">List of labels</a> | required |  |


<a id="sol_imports"></a>

## sol_imports

<pre>
sol_imports(<a href="#sol_imports-name">name</a>, <a href="#sol_imports-deps">deps</a>, <a href="#sol_imports-remappings">remappings</a>, <a href="#sol_imports-srcs">srcs</a>)
</pre>

Collect .sol source files to be imported as library code.
    Performs no actions, so semantically equivalent to filegroup().
    

**ATTRIBUTES**


| Name  | Description | Type | Mandatory | Default |
| :------------- | :------------- | :------------- | :------------- | :------------- |
| <a id="sol_imports-name"></a>name |  A unique name for this target.   | <a href="https://bazel.build/concepts/labels#target-names">Name</a> | required |  |
| <a id="sol_imports-deps"></a>deps |  Each dependency should either be more sol_imports, or npm packages for 3p dependencies   | <a href="https://bazel.build/concepts/labels">List of labels</a> | optional | [] |
| <a id="sol_imports-remappings"></a>remappings |  Contribute to import mappings.<br><br>        See https://docs.soliditylang.org/en/latest/path-resolution.html?highlight=remappings#import-remapping   | <a href="https://bazel.build/rules/lib/dict">Dictionary: String -> String</a> | optional | {} |
| <a id="sol_imports-srcs"></a>srcs |  Solidity source files   | <a href="https://bazel.build/concepts/labels">List of labels</a> | optional | [] |


<a id="sol_remappings"></a>

## sol_remappings

<pre>
sol_remappings(<a href="#sol_remappings-name">name</a>, <a href="#sol_remappings-deps">deps</a>, <a href="#sol_remappings-remappings">remappings</a>)
</pre>

sol_remappings combines remappings from multiple targets, and generates a Forge-compatible remappings.txt file.

**ATTRIBUTES**


| Name  | Description | Type | Mandatory | Default |
| :------------- | :------------- | :------------- | :------------- | :------------- |
| <a id="sol_remappings-name"></a>name |  A unique name for this target.   | <a href="https://bazel.build/concepts/labels#target-names">Name</a> | required |  |
| <a id="sol_remappings-deps"></a>deps |  sol_binary, sol_imports, or other sol_remappings targets from which remappings are combined.   | <a href="https://bazel.build/concepts/labels">List of labels</a> | required |  |
| <a id="sol_remappings-remappings"></a>remappings |  Additional import remappings.   | <a href="https://bazel.build/rules/lib/dict">Dictionary: String -> String</a> | optional | {} |


<a id="gather_transitive_imports"></a>

## gather_transitive_imports

<pre>
gather_transitive_imports(<a href="#gather_transitive_imports-ctx">ctx</a>)
</pre>



**PARAMETERS**


| Name  | Description | Default Value |
| :------------- | :------------- | :------------- |
| <a id="gather_transitive_imports-ctx"></a>ctx |  <p align="center"> - </p>   |  none |


<a id="write_remappings_info"></a>

## write_remappings_info

<pre>
write_remappings_info(<a href="#write_remappings_info-ctx">ctx</a>, <a href="#write_remappings_info-output_file">output_file</a>, <a href="#write_remappings_info-remappings_info">remappings_info</a>, <a href="#write_remappings_info-lib_dir">lib_dir</a>)
</pre>



**PARAMETERS**


| Name  | Description | Default Value |
| :------------- | :------------- | :------------- |
| <a id="write_remappings_info-ctx"></a>ctx |  <p align="center"> - </p>   |  none |
| <a id="write_remappings_info-output_file"></a>output_file |  <p align="center"> - </p>   |  none |
| <a id="write_remappings_info-remappings_info"></a>remappings_info |  <p align="center"> - </p>   |  none |
| <a id="write_remappings_info-lib_dir"></a>lib_dir |  <p align="center"> - </p>   |  <code>""</code> |


