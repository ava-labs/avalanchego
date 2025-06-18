//go:build ignore

package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
)

const (
	defaultMode = "LOCAL_LIBS"
)

// run via go generate to parse $GOFILE and scan for blocks of CGO directives (including cgo comments, which require a "// //" prefix)
// Uses delimiters set by:
// FIREWOOD_CGO_BEGIN_<FIREWOOD_LD_MODE>
// cgo line 1
// .
// .
// .
// cgo line n
// FIREWOOD_CGO_END_<FIREWOOD_LD_MODE>
//
// $GOFILE may contain multiple such blocks, where only one should be active at once.
// Discovers the set FIREWOOD_LD_MODE via env var or defaults to "LOCAL_LIBS", which is
// specific to the firewood.go file.
// Activates the block set by FIREWOOD_LD_MODE by uncommenting any commented out cgo directives
// and deactivates all other blocks' cgo directives.
func main() {
	mode := os.Getenv("FIREWOOD_LD_MODE")
	if mode == "" {
		mode = defaultMode
	}

	if err := switchCGOMode(mode); err != nil {
		log.Fatalf("Error switching CGO mode to %s: %v", mode, err)
	}
	fmt.Printf("Successfully switched CGO directives to %s mode\n", mode)
}

func getTargetFile() (string, error) {
	targetFile, ok := os.LookupEnv("GOFILE")
	if !ok {
		return "", fmt.Errorf("GOFILE is not set")
	}
	return targetFile, nil
}

type CGOBlock struct {
	Name      string   // e.g., "STATIC_LIBS", "LOCAL_LIBS"
	StartLine int      // Line number where block starts
	EndLine   int      // Line number where block ends
	Lines     []string // All lines in the block (including begin/end markers)
}

func switchCGOMode(targetMode string) error {
	targetFile, err := getTargetFile()
	if err != nil {
		return err
	}

	content, err := os.ReadFile(targetFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", targetFile, err)
	}

	lines := strings.Split(string(content), "\n")

	blocks, err := findCGOBlocks(lines)
	if err != nil {
		return fmt.Errorf("failed to find CGO blocks: %w", err)
	}

	if err := validateCGOBlocks(blocks, lines); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	cgoBlockMap, err := createCGOBlockMap(blocks)
	if err != nil {
		return err
	}

	targetCGOBlock, ok := cgoBlockMap[targetMode]
	if !ok {
		return fmt.Errorf("no CGO block found for FIREWOOD_LD_MODE=%s", targetMode)
	}

	// Process lines: activate target block, deactivate others
	result := make([]string, len(lines))
	copy(result, lines)

	for _, block := range blocks {
		isTarget := block.Name == targetCGOBlock.Name

		for i := block.StartLine + 1; i < block.EndLine; i++ {
			line := lines[i]

			if isTarget {
				// Activate: "// // #cgo" -> "// #cgo"
				result[i] = activateCGOLine(line)
			} else {
				// Deactivate: "// #cgo" -> "// // #cgo"
				result[i] = deactivateCGOLine(line)
			}
		}
	}

	newContent := strings.Join(result, "\n")
	return os.WriteFile(targetFile, []byte(newContent), 0644)
}

func findCGOBlocks(lines []string) ([]CGOBlock, error) {
	var blocks []CGOBlock
	beginRegex := regexp.MustCompile(`^// // FIREWOOD_CGO_BEGIN_(\w+)`)
	endRegex := regexp.MustCompile(`^// // FIREWOOD_CGO_END_(\w+)`)

	for i, line := range lines {
		matches := beginRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		blockName := matches[1]

		// Find corresponding end marker
		endLine := -1
		for j := i + 1; j < len(lines); j++ {
			if endMatches := endRegex.FindStringSubmatch(lines[j]); endMatches != nil {
				if endMatches[1] == blockName {
					endLine = j
					break
				}
			}
		}

		if endLine == -1 {
			return nil, fmt.Errorf("no matching end marker found for FIREWOOD_CGO_BEGIN_%s at line %d", blockName, i+1)
		}

		blocks = append(blocks, CGOBlock{
			Name:      blockName,
			StartLine: i,
			EndLine:   endLine,
			Lines:     lines[i : endLine+1],
		})
	}

	return blocks, nil
}

func validateCGOBlocks(blocks []CGOBlock, lines []string) error {
	// Check every CGO block contains ONLY valid CGO directives or valid comments within a CGO directive block
	for _, block := range blocks {
		for i := block.StartLine + 1; i < block.EndLine; i++ {
			if !isCGODirective(lines[i]) {
				return fmt.Errorf("invalid CGO directive at line %d in block %s: %s", i+1, block.Name, lines[i])
			}
		}
	}

	return nil
}

func createCGOBlockMap(blocks []CGOBlock) (map[string]CGOBlock, error) {
	cgoBlockMap := make(map[string]CGOBlock)
	for _, block := range blocks {
		if existingBlock, exists := cgoBlockMap[block.Name]; exists {
			return nil, fmt.Errorf("duplicate CGO block name %q found at lines %d-%d, previously defined at lines %d-%d",
				block.Name, block.StartLine+1, block.EndLine+1,
				existingBlock.StartLine+1, existingBlock.EndLine+1)
		}
		cgoBlockMap[block.Name] = block
	}
	return cgoBlockMap, nil
}

func isCGODirective(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "// #cgo") || strings.HasPrefix(trimmed, "// // ")
}

func activateCGOLine(line string) string {
	// Convert "// // #cgo" to "// #cgo"
	if strings.Contains(line, "// // #cgo") {
		return strings.Replace(line, "// // #cgo", "// #cgo", 1)
	}
	// Already active
	return line
}

func deactivateCGOLine(line string) string {
	// Convert "// #cgo" to "// // #cgo" (but not "// // #cgo" to "// // // #cgo")
	if strings.Contains(line, "// #cgo") && !strings.Contains(line, "// // #cgo") {
		return strings.Replace(line, "// #cgo", "// // #cgo", 1)
	}
	// Already deactivated
	return line
}
