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
//
// This migration parses the Go AST and uses source-level heuristics to identify
// session.Store.Get/GetByID calls on variables initialized via session.NewStore(),
// including support for custom import aliases. It does not currently track stores
// that are passed via parameters, returned from functions, or stored in structs.
func MigrateSessionRelease(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		// Quick check: does file import session package?
		if !strings.Contains(content, "middleware/session") {
			return content
		}

		// Skip v2 imports - this migration only works with v3
		if strings.Contains(content, "fiber/v2/middleware/session") {
			return content
		}

		// Use type-based approach to find Store.Get() calls
		result, err := addReleaseCallsWithTypes(content, cwd)
		if err != nil {
			// Fallback: return original content if type checking fails
			return content
		}

		return result
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

// releasePoint represents a location where defer sess.Release() needs to be added
type releasePoint struct {
	indent  string // Indentation to use for defer statement
	sessVar string // Session variable name
	errVar  string // Error variable name
	line    int    // Line number where Get/GetByID was called
}

// addReleaseCallsWithTypes adds defer Release() statements for Store.Get() calls
func addReleaseCallsWithTypes(content, _ string) (string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "temp.go", content, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse file: %w", err)
	}

	points := findReleasePoints(file, fset, content)
	if len(points) == 0 {
		return content, nil
	}

	return insertDeferStatements(content, points), nil
}

// findReleasePoints analyzes the AST to find Store.Get() calls
func findReleasePoints(file *ast.File, fset *token.FileSet, src string) []releasePoint {
	var points []releasePoint

	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		if len(assign.Lhs) != 2 || len(assign.Rhs) != 1 || assign.Tok != token.DEFINE {
			return true
		}

		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		methodName := sel.Sel.Name
		if methodName != "Get" && methodName != "GetByID" {
			return true
		}

		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		if !isSessionStoreInFunction(src, ident.Name, fset, assign) {
			return true
		}

		sessIdent, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}

		var errVarName string
		if errIdent, ok := assign.Lhs[1].(*ast.Ident); ok {
			errVarName = errIdent.Name
		} else {
			errVarName = "_"
		}

		pos := fset.Position(assign.Pos())

		points = append(points, releasePoint{
			line:    pos.Line - 1,
			sessVar: sessIdent.Name,
			errVar:  errVarName,
			indent:  "",
		})

		return true
	})

	return points
}

// isSessionStoreInFunction checks if the given variable name appears to be a session store
// within the same function as the provided assignment statement
func isSessionStoreInFunction(src, varName string, fset *token.FileSet, assign *ast.AssignStmt) bool {
	aliases := findSessionPackageAliases(src)

	pos := fset.Position(assign.Pos())
	assignLine := pos.Line

	fnStart, fnEnd := findFunctionBoundaries(src, assignLine)
	if fnStart == -1 || fnEnd == -1 {
		return checkStoreAssignment(src, varName, aliases)
	}

	lines := strings.Split(src, "\n")
	fnLines := lines[fnStart:fnEnd]
	fnSrc := strings.Join(fnLines, "\n")

	return checkStoreAssignment(fnSrc, varName, aliases)
}

// checkStoreAssignment verifies the variable is assigned from session.NewStore()
func checkStoreAssignment(src, varName string, aliases []string) bool {
	for _, alias := range aliases {
		// Match: store := session.NewStore() or var store = session.NewStore()
		pattern := regexp.MustCompile(fmt.Sprintf(`\b%s\b\s*(?::=|=)\s*%s\.NewStore\(`, regexp.QuoteMeta(varName), regexp.QuoteMeta(alias)))
		if pattern.MatchString(src) {
			return true
		}
	}
	return false
}

// findFunctionBoundaries finds the start and end line numbers of the function containing the given line
func findFunctionBoundaries(src string, lineNum int) (start, end int) {
	lines := strings.Split(src, "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return -1, -1
	}

	lineIdx := lineNum - 1

	fnStart := -1
	for i := lineIdx; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "func ") {
			fnStart = i
			break
		}
	}

	if fnStart == -1 {
		return -1, -1
	}

	fnEnd := len(lines)
	for i := fnStart + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "func ") {
			fnEnd = i
			break
		}
	}

	return fnStart, fnEnd
}

// findSessionPackageAliases extracts all aliases used for the session middleware package
func findSessionPackageAliases(src string) []string {
	var aliases []string

	lines := strings.Split(src, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, `"github.com/gofiber/fiber/v3/middleware/session"`) {
			if strings.HasPrefix(line, `"github.com/gofiber/fiber/v3/middleware/session"`) {
				aliases = append(aliases, "session")
			} else {
				parts := strings.Fields(line)
				if len(parts) >= 2 && parts[1] == `"github.com/gofiber/fiber/v3/middleware/session"` {
					aliases = append(aliases, parts[0])
				}
			}
		}
	}

	return aliases
}

// insertDeferStatements adds defer sess.Release() at appropriate locations
func insertDeferStatements(content string, points []releasePoint) string {
	lines := strings.Split(content, "\n")

	for i := range points {
		if points[i].line < len(lines) {
			line := lines[points[i].line]
			points[i].indent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		}
	}

	for i := len(points) - 1; i >= 0; i-- {
		p := points[i]

		if p.line >= len(lines) {
			continue
		}

		if hasExistingRelease(lines, p.line, p.sessVar) {
			continue
		}

		nextLineIdx := p.line + 1
		if nextLineIdx >= len(lines) {
			insertAt := p.line + 1
			deferStmt := p.indent + "defer " + p.sessVar + ".Release() " + releaseComment
			lines = append(lines[:insertAt], append([]string{deferStmt}, lines[insertAt:]...)...)
			continue
		}

		nextLine := strings.TrimSpace(lines[nextLineIdx])

		if strings.HasPrefix(nextLine, "if "+p.errVar+" != nil") {
			blockEnd := findErrorBlockEnd(lines, nextLineIdx)
			if blockEnd >= 0 && blockEnd < len(lines) {
				insertAt := blockEnd + 1
				deferStmt := p.indent + "defer " + p.sessVar + ".Release() " + releaseComment
				lines = append(lines[:insertAt], append([]string{deferStmt}, lines[insertAt:]...)...)
			}
		} else {
			insertAt := p.line + 1
			deferStmt := p.indent + "defer " + p.sessVar + ".Release() " + releaseComment
			lines = append(lines[:insertAt], append([]string{deferStmt}, lines[insertAt:]...)...)
		}
	}

	return strings.Join(lines, "\n")
}

// hasExistingRelease checks if defer sess.Release() already exists for this session variable
func hasExistingRelease(lines []string, startLine int, sessVar string) bool {
	releaseCall := sessVar + ".Release()"

	searchStart := startLine - 2
	if searchStart < 0 {
		searchStart = 0
	}
	searchEnd := startLine + 5
	if searchEnd > len(lines) {
		searchEnd = len(lines)
	}

	for i := searchStart; i < searchEnd; i++ {
		if strings.Contains(lines[i], releaseCall) {
			return true
		}
	}

	return false
}

// findErrorBlockEnd finds the end of an error handling block using proper Go AST parsing.
// Returns the line index of the closing brace, or -1 if not found.
func findErrorBlockEnd(lines []string, startIdx int) int {
	if startIdx >= len(lines) {
		return -1
	}

	line := strings.TrimSpace(lines[startIdx])

	if strings.Contains(line, "{") && strings.Contains(line, "}") {
		return startIdx
	}

	if !strings.Contains(line, "{") {
		return -1
	}

	var sb strings.Builder
	sb.WriteString("package main\nfunc f() {\n") //nolint:errcheck // strings.Builder.WriteString never fails
	snippetStartLine := startIdx

	for i := startIdx; i < len(lines) && i < startIdx+50; i++ {
		sb.WriteString(lines[i]) //nolint:errcheck // strings.Builder.WriteString never fails
		sb.WriteString("\n")     //nolint:errcheck // strings.Builder.WriteString never fails
	}
	sb.WriteString("\n}\n") //nolint:errcheck // strings.Builder.WriteString never fails
	codeSnippet := sb.String()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", codeSnippet, parser.AllErrors)
	if err != nil {
		return findErrorBlockEndFallback(lines, startIdx)
	}

	ifStmtEnd := -1
	ast.Inspect(node, func(n ast.Node) bool {
		if ifStmt, ok := n.(*ast.IfStmt); ok {
			pos := fset.Position(ifStmt.Body.End())
			lineNum := pos.Line - 3
			if lineNum >= 0 {
				ifStmtEnd = snippetStartLine + lineNum
				return false
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
// Only used when AST parsing fails.
func findErrorBlockEndFallback(lines []string, startIdx int) int {
	braceCount := 1
	inString := false
	var stringChar byte
	inComment := false
	isLineComment := false

	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]
		for j := 0; j < len(line); j++ {
			ch := line[j]

			// Handle string literals
			if inString {
				if ch == stringChar && (j == 0 || line[j-1] != '\\') {
					inString = false
				}
				continue
			}

			// Handle comments
			if inComment {
				if isLineComment && ch == '\n' {
					inComment = false
					isLineComment = false
				} else if !isLineComment && ch == '*' && j+1 < len(line) && line[j+1] == '/' {
					inComment = false
					j++ // skip the '/'
				}
				continue
			}

			// Check for start of string or comment
			switch ch {
			case '"', '\'', '`':
				inString = true
				stringChar = ch
			case '/':
				if j+1 >= len(line) {
					continue
				}
				switch line[j+1] {
				case '/':
					inComment = true
					isLineComment = true
					j++ // skip the second '/'
				case '*':
					inComment = true
					isLineComment = false
					j++ // skip the '*'
				default:
					// Not a comment, continue
				}
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
