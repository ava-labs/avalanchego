// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// go generate script
//
// This script updates the cgo pragmas for each C function call site in the Go
// source files. It should be run after any change to the C function signatures
// in the Rust code, and after any change to the Go source files that adds or
// removes calls to C functions.
//
// The script scans for calls to C functions in the Go source files, identifies
// the corresponding cgo declaration (import "C"), and injects the appropriate
// cgo pragmas for each C function call site.
//
// The script relies on the convention that C function calls are made via
// identifiers with the prefix `_Cfunc_`, which is the default naming scheme
// used by cgo for C function calls. For example, a call to `C.fwd_get_from_revision`
// would be identified as a call to the C function `fwd_get_from_revision` via
// the identifier `_Cfunc_fwd_get_from_revision`.
//
// The script ensures that each C function has at most one callsite.
//
// The script also removes any old cgo pragmas that do not match the current set
// of C functions being called, to ensure that the source files are clean and
// up-to-date.
//
// The pragmas are injected in declaration order, i.e., the first C function
// call site in the file will have its pragmas injected first. This is
// deterministic as it relies on the order of AST traversal. Pre-sorting the C
// functions by name before injection is not necessary; however, refactoring the
// source would result in a different injection order.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

var pragmaComments = []string{
	"// #cgo noescape ",
	"// #cgo nocallback ",
}

var ignoredIdentifiers = []string{
	// GoBytes is a cgo helper function and not one produced by our headers;
	// however, it follows the same `_Cfunc_...` naming pattern.
	"GoBytes",
}

var (
	dir   string
	debug bool
)

func debugf(format string, args ...any) {
	if debug {
		log.Printf("DEBUG: %s", fmt.Sprintf(format, args...))
	}
}

func fatalf(format string, args ...any) {
	log.Fatalf("FATAL: %s", fmt.Sprintf(format, args...))
}

func fatalJoinErrorsf[T error](errs []T, format string, args ...any) {
	if err := joinErrors(errs); err != nil {
		fatalf("%s: %v", fmt.Sprintf(format, args...), err)
	}
}

func joinErrors[T error](errs []T) error {
	errList := make([]error, len(errs))
	for i, err := range errs {
		errList[i] = error(err)
	}

	return errors.Join(errList...)
}

func main() {
	log.SetFlags(log.Lshortfile | log.Ltime)
	flag.StringVar(&dir, "dir", "", "root directory of the firewood/ffi repository")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.Parse()

	if !debug && os.Getenv("CI") != "" {
		debug = true
		debugf("enabling debug logging because CI environment variable is set")
	}

	if dir == "" {
		if file := os.Getenv("GOFILE"); file != "" {
			dir = filepath.Dir(file)
			debugf("inferred target directory %s from GOFILE environment variable", dir)
		}
	}

	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			fatalf("-dir not provided and unable to get current working directory: %v", err)
		}
		debugf("defaulting to current working directory %s", dir)
	}

	ctx := context.Background()
	fset := token.NewFileSet()
	config := packages.Config{
		Mode:    packages.LoadAllSyntax,
		Context: ctx,
		Dir:     dir,
		Fset:    fset,
		Tests:   false, // test code should not directly invoke cgo functions
	}

	debugf("loading packages in %s", dir)
	pkgs, err := packages.Load(&config, ".")
	if err != nil {
		fatalf("failed to load packages: %v", err)
	}

	var results []fileResult
	cFuncCallSites := make(map[string][]string)

	// There is only one package; however, process in a loop in case of multiple
	// packages in the future.
	for _, pkg := range pkgs {
		debugf("parsed package %s", pkg.ID)
		fatalJoinErrorsf(pkg.Errors, "errors occurred when parsing package %s", pkg.ID)
		fatalJoinErrorsf(pkg.TypeErrors, "type errors occurred when parsing go source for package %s", pkg.ID)

		for _, astFile := range pkg.Syntax {
			filename := fset.Position(astFile.Pos()).Filename
			// select only ast files within `pkg.GoFiles` and ignore the
			// `CompiledGoFiles` (auto-generated cgo sources)
			if !slices.Contains(pkg.GoFiles, filename) {
				continue
			}

			v := walkFile(astFile)
			results = append(results, v)
			for _, cf := range v.cFuncs {
				callsite := cf.CallSite(fset)
				debugf("found call to C function %s at %s", cf.name, callsite)
				cFuncCallSites[cf.name] = append(cFuncCallSites[cf.name], callsite)
			}
		}
	}

	// Ensure each C function is called at most once across all files.
	var dupes []string
	for name, callsites := range cFuncCallSites {
		if len(callsites) > 1 {
			slices.Sort(callsites)
			dupes = append(dupes, fmt.Sprintf("  %s called from: %s", name, strings.Join(callsites, ", ")))
		}
	}
	if len(dupes) > 0 {
		slices.Sort(dupes)
		fatalf("C functions called from multiple Go files:\n%s", strings.Join(dupes, "\n"))
	}

	// Process each file: remove old pragmas and inject new ones.
	for _, r := range results {
		if r.cDecl == nil {
			continue
		}

		filename := fset.Position(r.goFile.Package).Filename
		if err := r.rewriteFile(fset); err != nil {
			fatalf("failed to rewrite %s: %v", filename, err)
		}

		debugf("successfully updated pragmas in file %s", filename)
	}
}

type fileResult struct {
	goFile *ast.File
	cDecl  *ast.GenDecl
	cFuncs []cFunctionCallSite
}

func (r fileResult) rewriteFile(fset *token.FileSet) error {
	cDeclPos := fset.Position(r.cDecl.Pos())
	filename := cDeclPos.Filename

	originalFile, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read original file: %w", err)
	}

	lines := strings.SplitAfter(string(originalFile), "\n")

	declLine := lines[cDeclPos.Line-1]
	debugf("processing file %s: found cgo declaration at line %d: %s", filename, cDeclPos.Line, declLine)
	if !strings.Contains(declLine, "import \"C\"") {
		return fmt.Errorf("unexpectedly did not find import \"C\" in line %d of %s", cDeclPos.Line, filename)
	}

	nDeleted := 0
	for i := cDeclPos.Line - 2; i >= 0; i-- {
		line := lines[i]
		// Stop processing lines once we reach a non-comment line. The comments
		// before the `import "C"` declaration are expected to all be
		// consecutive lines and adjacent to the declaration. Non-comment lines
		// or blank lines indicate the end of the comment block.
		if !strings.HasPrefix(line, "//") {
			break
		}

		// only delete lines in the comment block that look like cgo pragmas.
		// This allows for other comments to be included in the block without
		// being stripped by the script.
		if isPragmaComment(line) {
			debugf("removing old pragma comment at line %d: %s", i+1, line)
			lines = slices.Delete(lines, i, i+1)
			nDeleted++
		}
	}

	injectedComments := make([]string, 0, len(pragmaComments)*len(r.cFuncs))
	for _, cf := range r.cFuncs {
		for _, prefix := range pragmaComments {
			injectedComments = append(injectedComments, prefix+cf.name+"\n")
		}
	}

	lines = slices.Insert(lines, cDeclPos.Line-1-nDeleted, injectedComments...)

	// #nosec G306 - permissions are correct for source files
	// #nosec G703 - path is safe because it is controlled by the build system
	// and not user input
	return os.WriteFile(filename, []byte(strings.Join(lines, "")), 0o644)
}

func isPragmaComment(s string) bool {
	for _, prefix := range pragmaComments {
		if strings.HasPrefix(strings.TrimSpace(s), prefix) {
			return true
		}
	}
	return false
}

type fileVisitor struct {
	scope  scope
	cDecl  *ast.GenDecl
	cFuncs []cFunctionCallSite
}

func walkFile(file *ast.File) fileResult {
	var v fileVisitor
	ast.Walk(&v, file)
	return fileResult{
		goFile: file,
		cDecl:  v.cDecl,
		cFuncs: v.cFuncs,
	}
}

func (v *fileVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		// Visit(nil) is called at the end of each branch of the AST, so we pop
		// the scope stack here.
		v.scope.pop()
		return v
	}

	v.scope.push(node)

	switch n := node.(type) {
	case *ast.ImportSpec:
		// the underscore is used instead of `C` when parsed.
		if n.Name == nil || n.Name.Name != "_" {
			break
		}

		if v.cDecl != nil {
			fatalf("unexpected multiple cgo declarations in a single file")
		}

		// the parent of the ImportSpec is the GenDecl for `import "C"`
		v.cDecl = v.scope.parent().(*ast.GenDecl)
	case *ast.CallExpr:
		ident, ok := ast.Unparen(n.Fun).(*ast.Ident)
		if !ok {
			break
		}

		name, ok := strings.CutPrefix(ident.Name, "_Cfunc_")
		if !ok {
			break
		}

		if slices.Contains(ignoredIdentifiers, name) {
			debugf("ignoring call to C function %s", name)
			break
		}

		v.cFuncs = append(v.cFuncs, cFunctionCallSite{
			name:  name,
			ident: ident,
		})
	default:
		// do nothing
	}

	return v
}

type cFunctionCallSite struct {
	name string
	// ident will have the prefix `_Cfunc_` followed by the name of the C
	// function being called, e.g., `_Cfunc_fwd_get_from_revision`.
	ident *ast.Ident
}

func (c *cFunctionCallSite) CallSite(fset *token.FileSet) string {
	return fset.Position(c.ident.NamePos).String()
}

type scope []ast.Node

func (s *scope) isEmpty() bool {
	return len(*s) == 0
}

func (s *scope) push(node ast.Node) {
	*s = append(*s, node)
}

func (s *scope) pop() {
	if s.isEmpty() {
		fatalf("unexpected pop from empty scope stack")
	}

	*s = (*s)[:len(*s)-1]
}

func (s *scope) parent() ast.Node {
	if len(*s) < 2 {
		return nil
	}

	return (*s)[len(*s)-2]
}
