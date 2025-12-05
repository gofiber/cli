package v3

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

const releaseComment = "// Important: Manual cleanup required"

// MigrateSessionRelease adds defer sess.Release() after store.Get() calls
// when using the Store Pattern (legacy pattern).
// This is required in v3 for manual session lifecycle management.
//
// Only the following Store methods return *Session from the pool and require Release():
//   - store.Get(c fiber.Ctx) (*Session, error)
//   - store.GetByID(ctx context.Context, id string) (*Session, error)
//
// Middleware handlers do NOT require Release() as the middleware manages the lifecycle.
func MigrateSessionRelease(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		lines := strings.Split(content, "\n")

		// Step 1: Find session package import and its alias
		sessionPkgAlias := findSessionPackageAlias(lines)
		if sessionPkgAlias == "" {
			// No session package imported, skip this file
			return content
		}

		// Step 2: Find all Store variable names created from session package
		storeVars := findSessionStoreVariables(lines, sessionPkgAlias)
		if len(storeVars) == 0 {
			// No session stores found, skip this file
			return content
		}

		// Step 3: Process the file and add Release() calls where needed
		return addReleaseCalls(lines, storeVars, sessionPkgAlias)
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

// findSessionPackageAlias finds the alias used for the session package import.
// Returns the alias (e.g., "session", "sshadow") or empty string if not found.
// Note: This migration runs AFTER MigrateContribPackages, so imports are already v3.
func findSessionPackageAlias(lines []string) string {
	// Match: import "github.com/gofiber/fiber/v3/middleware/session"
	// Or:    import sessionAlias "github.com/gofiber/fiber/v3/middleware/session"
	reSessionImport := regexp.MustCompile(`^\s*(?:(\w+)\s+)?"github\.com/gofiber/fiber/v3/middleware/session"`)

	for _, line := range lines {
		matches := reSessionImport.FindStringSubmatch(line)
		if len(matches) > 0 {
			if matches[1] != "" {
				// Custom alias
				return matches[1]
			}
			// Default alias is the package name
			return "session"
		}
	}
	return ""
}

// findSessionStoreVariables finds all variable names that are session.NewStore().
// Returns a map of variable names that are session stores.
// Note: This migration runs AFTER MigrateSessionStore, so session.New() has already
// been converted to session.NewStore().
func findSessionStoreVariables(lines []string, sessionPkgAlias string) map[string]bool {
	storeVars := make(map[string]bool)

	// Match patterns like:
	//   store := session.NewStore()
	//   var store = session.NewStore()
	//   var store *session.Store
	//   myStore := session.NewStore(config)
	reStoreNewStore := regexp.MustCompile(fmt.Sprintf(`^\s*(?:var\s+)?(\w+)\s*(?::=|=)\s*%s\.NewStore\(`, regexp.QuoteMeta(sessionPkgAlias)))
	reStoreType := regexp.MustCompile(fmt.Sprintf(`^\s*(?:var\s+)?(\w+)\s+\*?%s\.Store`, regexp.QuoteMeta(sessionPkgAlias)))

	for _, line := range lines {
		// Check for NewStore() calls
		if matches := reStoreNewStore.FindStringSubmatch(line); len(matches) > 1 {
			storeVars[matches[1]] = true
			continue
		}

		// Check for *Store type declarations
		if matches := reStoreType.FindStringSubmatch(line); len(matches) > 1 {
			storeVars[matches[1]] = true
		}
	}

	return storeVars
}

// isSessionStoreInScope verifies that a store variable is actually from session.NewStore()
// within the current function scope by looking backwards from the Get() call.
// This prevents false positives when the same variable name is reused in different functions.
func isSessionStoreInScope(lines []string, getLineIdx int, storeVar string, storeVars map[string]bool, sessionPkgAlias string) bool {
	// The store variable name must be in our tracked list
	if !storeVars[storeVar] {
		return false
	}

	// Pre-compile regex patterns for efficiency
	storeAssignPattern := regexp.MustCompile(fmt.Sprintf(`^\s*(?:var\s+)?%s\s*(?::=|=)\s*(\w+)\.NewStore\(`, regexp.QuoteMeta(storeVar)))
	sessionPattern := regexp.MustCompile(fmt.Sprintf(`\b%s\.NewStore\(`, regexp.QuoteMeta(sessionPkgAlias)))
	namedFuncPattern := regexp.MustCompile(`^func(\s+\([^\)]*\))?\s+\w+\s*\(`)

	// Look backwards to find where this store variable was assigned
	// Allow searching through closures into parent scope (closures can access parent variables)
	for i := getLineIdx - 1; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Check if this line assigns the store variable
		if matches := storeAssignPattern.FindStringSubmatch(line); len(matches) > 1 {
			// Found the assignment - verify it's from the session package
			pkgAlias := matches[1]
			if pkgAlias == sessionPkgAlias {
				// This is the session store we are looking for
				return true
			}
			// This is a store from another package, shadowing the variable
			return false
		}

		// Alternative check using pattern matching (for robustness)
		if storeAssignPattern.MatchString(line) && sessionPattern.MatchString(line) {
			return true
		}

		// Stop if we've reached a named function declaration (including method receivers)
		// This matches: func Name(, func (r Receiver) Name(, but NOT func( or func (
		// Closures (anonymous functions) are allowed - we can search through them
		if namedFuncPattern.MatchString(trimmed) {
			// We've hit a named function, stop
			return false
		}
	}

	return false
}

const releaseSearchAhead = 30 // Lines to search ahead for existing Release() calls

// addReleaseCalls processes lines and adds defer Release() calls after store.Get()/GetByID() calls.
func addReleaseCalls(lines []string, storeVars map[string]bool, sessionPkgAlias string) string {
	// Build regex pattern that only matches our known store variables
	storeNames := make([]string, 0, len(storeVars))
	for name := range storeVars {
		storeNames = append(storeNames, regexp.QuoteMeta(name))
	}

	if len(storeNames) == 0 {
		return strings.Join(lines, "\n")
	}

	// Match: sessVar, errVar := (store|sessionStore|myStore).Get(...) or .GetByID(...)
	storePattern := strings.Join(storeNames, "|")
	reStoreGet := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*)(\w+),\s*(\w+)\s*:=\s*(%s)\.(Get(?:ByID)?)\(`, storePattern))

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
		storeVar := matches[4]

		// CRITICAL: Verify this store variable is actually from session.NewStore()
		// in the current function scope to avoid false positives across functions
		if !isSessionStoreInScope(lines, i, storeVar, storeVars, sessionPkgAlias) {
			continue
		}

		// Check if Release() is already present for this session variable
		// Search from right after the Get() line
		hasRelease := false
		searchEnd := i + releaseSearchAhead
		if searchEnd > len(lines) {
			searchEnd = len(lines)
		}
		for j := i + 1; j < searchEnd; j++ {
			if strings.Contains(lines[j], sessVar+".Release()") {
				hasRelease = true
				break
			}
		}

		if hasRelease {
			continue
		}

		// Look for the error check pattern after this line
		nextLineIdx := i + 1
		if nextLineIdx >= len(lines) {
			// End of file - add defer right after the Get() call
			deferLine := indent + "defer " + sessVar + ".Release() " + releaseComment
			result = append(result, deferLine)
			continue
		}

		nextLine := strings.TrimSpace(lines[nextLineIdx])

		// Check if the next line starts an error check
		if strings.HasPrefix(nextLine, "if "+errVar+" != nil") {
			// Find where the error block ends
			blockEnd := findErrorBlockEnd(lines, nextLineIdx)

			if blockEnd < 0 || blockEnd >= len(lines) {
				// Fallback: add defer immediately after Get() if we can't parse the error block
				deferLine := indent + "defer " + sessVar + ".Release() " + releaseComment
				result = append(result, deferLine)
				continue
			}

			// Insert defer after the error block
			deferLine := indent + "defer " + sessVar + ".Release() " + releaseComment

			// Skip ahead in the loop to include all lines up to blockEnd
			for i < blockEnd {
				i++
				if i < len(lines) {
					result = append(result, lines[i])
				}
			}

			// Now insert the defer line
			result = append(result, deferLine)
		} else {
			// No error check - add defer immediately after the Get() call
			deferLine := indent + "defer " + sessVar + ".Release() " + releaseComment
			result = append(result, deferLine)
		}
	}

	return strings.Join(result, "\n")
}

// findErrorBlockEnd finds the end of an error handling block using proper Go AST parsing.
// Returns the line index of the closing brace, or -1 if not found.
// This properly handles braces in strings, comments, and nested blocks.
func findErrorBlockEnd(lines []string, startIdx int) int {
	if startIdx >= len(lines) {
		return -1
	}

	line := strings.TrimSpace(lines[startIdx])

	// Check if it's a single-line if statement
	if strings.Contains(line, "{") && strings.Contains(line, "}") {
		return startIdx
	}

	// For multi-line blocks, we need to find the matching closing brace
	// Use Go's parser to properly handle this
	if !strings.Contains(line, "{") {
		return -1
	}

	// Build a minimal parseable snippet starting from the if statement
	// We need enough context to parse it as valid Go
	var sb strings.Builder
	sb.WriteString("package main\nfunc f() {\n") //nolint:errcheck // strings.Builder.WriteString never returns an error
	snippetStartLine := startIdx

	// Add lines from the error check onwards
	for i := startIdx; i < len(lines) && i < startIdx+50; i++ {
		sb.WriteString(lines[i]) //nolint:errcheck // strings.Builder.WriteString never returns an error
		sb.WriteString("\n")     //nolint:errcheck // strings.Builder.WriteString never returns an error
	}
	sb.WriteString("\n}\n") //nolint:errcheck // strings.Builder.WriteString never returns an error
	codeSnippet := sb.String()

	// Parse the code snippet
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", codeSnippet, parser.AllErrors)
	if err != nil {
		// Fallback: if we can't parse, use simple brace counting
		// This should rarely happen with valid Go code
		return findErrorBlockEndFallback(lines, startIdx)
	}

	// Find the if statement node
	ifStmtEnd := -1
	ast.Inspect(node, func(n ast.Node) bool {
		if ifStmt, ok := n.(*ast.IfStmt); ok {
			// Get the position of the closing brace of the if block
			pos := fset.Position(ifStmt.Body.End())
			// Subtract the offset we added (package main + func f() {)
			lineNum := pos.Line - 3 // 3 lines: package, blank, func
			if lineNum >= 0 {
				ifStmtEnd = snippetStartLine + lineNum - 1
				return false // Found it, stop searching
			}
		}
		return true
	})

	if ifStmtEnd >= 0 && ifStmtEnd < len(lines) {
		return ifStmtEnd
	}

	return findErrorBlockEndFallback(lines, startIdx)
}

// findErrorBlockEndFallback is a simple fallback that counts braces.
// Only used when AST parsing fails (which should be rare with valid Go code).
func findErrorBlockEndFallback(lines []string, startIdx int) int {
	braceCount := 1
	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]
		// Simple character-by-character scan (doesn't handle strings/comments)
		for _, ch := range line {
			switch ch {
			case '{':
				braceCount++
			case '}':
				braceCount--
				if braceCount == 0 {
					return i
				}
			default:
				// Ignore other characters
			}
		}
	}
	return -1
}
