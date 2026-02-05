# Refactor tmpnet and e2e docs

## Files to target
 - e2e readme ../tests/e2e/README.md
 - tmpnet readme ../tests/fixture/tmpnet/README.md

## Steps

 - Examine the files rooted at ../tests/fixture/tmpnet (i.e. including subdirectories) and determine the gaps in the documentation in the tmpnet readme
   - Recent changes include:
     - The addition of the kube runtime (kube_runtime.go). The mechanics of the runtime can likely be gleaned from the code and comments in the code, but where things are not clear, ask questions
     - Revamping of available flags (tmpnet/flags). This probably suggests a new section in the readme documenting all the available flags
 - Propose changes to fill the gaps identified in the tmpnet readme
 - Examine the e2e readme and move tmpnet-specific details to the tmpnet readme (deduping where necessary) and add relevant links from the e2e readme to the tmpnet readme
   - especially relevant will be the section on flags
