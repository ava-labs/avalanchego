# Genesis generator

This tool is used to generate genesis JSON file for the network.

**USAGE:**

```bash
genesis_generator \
  PATH_TO_XLSX_FILE \
  PATH_TO_JSON_TEMPLATE \
  NETWORK_NAME \
  OUTPUT_PATH
```
where 
- `NETWORK_NAME` has to be one of: `kopernikus`, `columbus` or `camino`.
- `OUTPUT_PATH` should be a path to the directory where the genesis file will be saved.

Tool produces a file `genesis_<NETWORK_NAME>.json` in the `OUTPUT_PATH` directory.
<br/>**:warning: Tool was not tested with Windows paths.**

Tool assumes multisignature definitions and allocations are provided in the Excel file. Any other information that shall be contained in the resulting genesis file must be provided in the JSON template.

**ARTIFACTS:**

Inside this folder the following genesis files are stored in subfolders. There are:
- `templates` - with template file for the generator
- `generated` - generated files for the networks.

**MORE INFO:**

Just read the code. It's not that long.
