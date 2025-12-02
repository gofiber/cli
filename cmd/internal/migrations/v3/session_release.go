package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateSessionRelease adds defer sess.Release() after store.Get() calls
// when using the Store Pattern (legacy pattern).
// This is required in v3 for manual session lifecycle management.
func MigrateSessionRelease(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	// Match patterns like:
	//   sess, err := store.Get(c)
	//   sess, err := store.GetByID(ctx, sessionID)
	//   session, err := myStore.Get(c)
	// Capture: variable name, store variable name, method call
	reStoreGet := regexp.MustCompile(`(?m)^(\s*)(\w+),\s*(\w+)\s*:=\s*(\w+)\.(Get(?:ByID)?)\(`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		lines := strings.Split(content, "\n")
		result := make([]string, 0, len(lines))

		for i := 0; i < len(lines); i++ {
			line := lines[i]
			result = append(result, line)

			// Check if this line matches a store.Get() call
			matches := reStoreGet.FindStringSubmatch(line)
			if len(matches) < 6 {
				continue
			}

			indent := matches[1]
			sessVar := matches[2]
			errVar := matches[3]

			// Look for the error check pattern after this line
			// Common patterns:
			//   if err != nil {
			//   if err != nil { return ... }
			nextLineIdx := i + 1
			if nextLineIdx >= len(lines) {
				continue
			}

			nextLine := strings.TrimSpace(lines[nextLineIdx])

			// Check if the next line starts an error check
			if !strings.HasPrefix(nextLine, "if "+errVar+" != nil") {
				continue
			}

			// Find where the error block ends
			blockEnd := findErrorBlockEnd(lines, nextLineIdx, indent)

			// Insert defer after the error block
			if blockEnd < 0 || blockEnd >= len(lines) {
				continue
			}

			// Check if there's already a defer sess.Release() after the error block
			hasRelease := false
			searchEnd := blockEnd + 20
			if searchEnd > len(lines) {
				searchEnd = len(lines)
			}
			for j := blockEnd + 1; j < searchEnd; j++ {
				if strings.Contains(lines[j], sessVar+".Release()") {
					hasRelease = true
					break
				}
				// Stop searching if we hit a closing brace at the same or lower indent level
				// Only stop on lines that are purely closing braces (possibly with trailing comments)
				trimmed := strings.TrimSpace(lines[j])
				if strings.HasPrefix(trimmed, "}") && !strings.Contains(trimmed, "{") && !strings.Contains(trimmed, "else") {
					break
				}
			}

			if hasRelease {
				// Skip ahead to avoid re-processing these lines
				for i < blockEnd {
					i++
					if i < len(lines) {
						result = append(result, lines[i])
					}
				}
				continue
			}

			// Insert the defer statement after the error block
			deferLine := indent + "defer " + sessVar + ".Release() // Important: Manual cleanup required"

			// Skip ahead in the loop to include all lines up to blockEnd
			for i < blockEnd {
				i++
				if i < len(lines) {
					result = append(result, lines[i])
				}
			}

			// Now insert the defer line
			result = append(result, deferLine)
		}

		return strings.Join(result, "\n")
	})
	if err != nil {
		return fmt.Errorf("failed to add session Release() calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Adding defer sess.Release() for Store Pattern usage")
	return nil
}

// findErrorBlockEnd finds the end of an error handling block
// Returns the line index of the closing brace, or -1 if not found
// Note: This uses simple brace counting and may not handle braces in strings/comments,
// but is sufficient for migration purposes with typical Go error handling patterns.
func findErrorBlockEnd(lines []string, startIdx int, _ string) int {
	if startIdx >= len(lines) {
		return -1
	}

	line := strings.TrimSpace(lines[startIdx])

	// Check if it's a single-line if statement
	if strings.Contains(line, "{") && strings.Contains(line, "}") {
		return startIdx
	}

	// Multi-line block: find the matching closing brace
	if strings.Contains(line, "{") {
		braceCount := 1
		for i := startIdx + 1; i < len(lines); i++ {
			currLine := lines[i]
			braceCount += strings.Count(currLine, "{")
			braceCount -= strings.Count(currLine, "}")

			if braceCount == 0 {
				return i
			}
		}
	}

	return -1
}
