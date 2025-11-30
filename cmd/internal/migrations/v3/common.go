package v3

import (
	"fmt"
	"go/ast"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	hexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	b64Re = regexp.MustCompile(`^[A-Za-z0-9+/]{43}=?$`)
)

func isIdentifierChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

// skipCommaSuffix advances the index past a comma and any trailing
// whitespace, comments, or newline characters.
func skipCommaSuffix(src string, i int) int {
	i++
	for i < len(src) {
		switch src[i] {
		case ' ', '\t':
			i++
		case '/':
			if i+1 < len(src) {
				if src[i+1] == '/' { // line comment
					i += 2
					for i < len(src) && src[i] != '\n' {
						i++
					}
					if i < len(src) {
						return i + 1
					}
					return i
				}
				if src[i+1] == '*' { // block comment
					i += 2
					for i+1 < len(src) && (src[i] != '*' || src[i+1] != '/') {
						i++
					}
					if i+1 < len(src) {
						i += 2
						continue
					}
					return i
				}
			}
			return i
		case '\n':
			return i + 1
		default:
			return i
		}
	}
	return i
}

// removeConfigField deletes a field assignment from a struct literal. It
// handles nested parentheses and braces so it can process complex values like
// multi-line function literals or function calls with arguments that contain
// commas.
func removeConfigField(src, field string) string {
	re := regexp.MustCompile(field + `:\s*`)
	for {
		loc := re.FindStringIndex(src)
		if loc == nil {
			break
		}

		start := loc[0]
		// include any leading whitespace and comma, but keep the preceding
		// newline intact so that formatting is preserved for the previous
		// field when the target field is removed.
		for start > 0 {
			switch src[start-1] {
			case ' ', '\t':
				start--
			case ',':
				start--
				for start > 0 && (src[start-1] == ' ' || src[start-1] == '\t') {
					start--
				}
				goto leadDone
			default:
				goto leadDone
			}
		}
	leadDone:

		i := loc[1]
		depth := 0
		inString := false
		for i < len(src) {
			ch := src[i]
			if inString {
				if ch == '\\' && i+1 < len(src) {
					i += 2
					continue
				}
				if ch == '"' {
					inString = false
				}
				i++
				continue
			}

			switch ch {
			case '"':
				inString = true
			case '(', '{', '[':
				depth++
			case ')', '}', ']':
				if depth > 0 {
					depth--
					i++
					continue
				}
				if ch == '}' {
					src = src[:start] + src[i:]
					goto nextField
				}
			case ',':
				if depth > 0 {
					i++
					continue
				}
				i = skipCommaSuffix(src, i)
				src = src[:start] + src[i:]
				goto nextField
			case '\n':
				if depth > 0 {
					i++
					continue
				}
				i++
				src = src[:start] + src[i:]
				goto nextField
			default:
			}
			i++
		}
		src = src[:start]
	nextField:
	}
	return src
}

// replaceKeyLookup searches a struct literal for a KeyLookup field and invokes
// fn with the parsed components. If fn returns an empty string, the field is
// removed entirely.
func replaceKeyLookup(src string, fn func(indent, val, comma, comment, newline string) string) string {
	return replaceStringField(src, "KeyLookup", fn)
}

func replaceStringField(src, field string, fn func(indent, val, comma, comment, newline string) string) string {
	return replaceFieldImpl(src, field, true, fn)
}

func replaceField(src, field string, fn func(indent, val, comma, comment, newline string) string) string {
	return replaceFieldImpl(src, field, false, fn)
}

func replaceFieldImpl(src, field string, unquote bool, fn func(indent, val, comma, comment, newline string) string) string {
	re := regexp.MustCompile(`(?m)^(\s*)` + regexp.QuoteMeta(field) + `:\s*([^\n]+)(\n?)`)
	return re.ReplaceAllStringFunc(src, func(s string) string {
		sub := re.FindStringSubmatch(s)
		indent := sub[1]
		val := strings.TrimSpace(sub[2])
		newline := sub[3]

		comment := ""
		if idx := strings.Index(val, "//"); idx >= 0 {
			comment = strings.TrimSpace(val[idx:])
			val = strings.TrimSpace(val[:idx])
		} else if idx := strings.Index(val, "/*"); idx >= 0 {
			comment = strings.TrimSpace(val[idx:])
			val = strings.TrimSpace(val[:idx])
		}

		comma := ""
		if strings.HasSuffix(val, ",") {
			comma = ","
			val = strings.TrimSpace(strings.TrimSuffix(val, ","))
		}

		if unquote {
			uq, err := strconv.Unquote(val)
			if err != nil {
				if comment != "" {
					return fmt.Sprintf("%s// TODO: migrate %s: %s %s%s", indent, field, val, comment, newline)
				}
				return fmt.Sprintf("%s// TODO: migrate %s: %s%s", indent, field, val, newline)
			}
			val = uq
		}

		repl := fn(indent, val, comma, comment, newline)
		if repl == "" {
			if comment != "" {
				return fmt.Sprintf("%s%s%s", indent, comment, newline)
			}
			return newline
		}
		return repl
	})
}

func collectAliases(content string, reImport *regexp.Regexp, defaults []string) []string {
	aliases := map[string]struct{}{}
	for _, m := range reImport.FindAllStringSubmatch(content, -1) {
		alias := strings.TrimSpace(m[1])
		if alias == "" {
			for _, d := range defaults {
				aliases[d] = struct{}{}
			}
			continue
		}
		aliases[alias] = struct{}{}
	}

	result := make([]string, 0, len(aliases))
	for alias := range aliases {
		result = append(result, alias)
	}
	sort.Strings(result)
	return result
}

// splitArgs splits a comma-separated argument list into its individual arguments
// while respecting nested parentheses, brackets, and braces as well as quoted
// strings. It returns the trimmed arguments without altering inner spacing.
func splitArgs(src string) []string {
	var args []string
	depthPar, depthBrk, depthBrc := 0, 0, 0
	inStr := false
	var quote byte
	start := 0

	for i := 0; i < len(src); i++ {
		ch := src[i]
		if inStr {
			if ch == '\\' {
				i++
				continue
			}
			if ch == quote {
				inStr = false
			}
			continue
		}

		switch ch {
		case '"', '\'', '`':
			inStr = true
			quote = ch
		case '(':
			depthPar++
		case ')':
			if depthPar > 0 {
				depthPar--
			}
		case '[':
			depthBrk++
		case ']':
			if depthBrk > 0 {
				depthBrk--
			}
		case '{':
			depthBrc++
		case '}':
			if depthBrc > 0 {
				depthBrc--
			}
		case ',':
			if depthPar == 0 && depthBrk == 0 && depthBrc == 0 {
				args = append(args, strings.TrimSpace(src[start:i]))
				start = i + 1
			}
		default:
		}
	}

	args = append(args, strings.TrimSpace(src[start:]))
	return args
}

// extractCall returns the index just after the closing parenthesis of the call
// that starts at start and the argument substring within the parentheses. It
// assumes that start points to the first character after the opening
// parenthesis.
func extractCall(src string, start int) (int, string) {
	depth := 1
	inStr := false
	var quote byte

	for i := start; i < len(src); i++ {
		ch := src[i]
		if inStr {
			if ch == '\\' {
				i++
				continue
			}
			if ch == quote {
				inStr = false
			}
			continue
		}

		switch ch {
		case '"', '\'', '`':
			inStr = true
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1, src[start:i]
			}
		default:
		}
	}
	return len(src), src[start:]
}

// extractBlock returns the index just after the closing delimiter that matches
// the opening delimiter at the start position. Start must point to the first
// character after the opening delimiter. It handles nested delimiters and
// quoted strings.
//
//nolint:unparam // keep open for potential future delimiters
func extractBlock(src string, start int, open, closeDelim byte) int {
	depth := 1
	inStr := false
	var quote byte

	for i := start; i < len(src); i++ {
		ch := src[i]
		if inStr {
			if ch == '\\' {
				i++
				continue
			}
			if ch == quote {
				inStr = false
			}
			continue
		}

		switch ch {
		case '"', '\'', '`':
			inStr = true
			quote = ch
		case open:
			depth++
		case closeDelim:
			depth--
			if depth == 0 {
				return i + 1
			}
		default:
		}
	}
	return len(src)
}

// replaceCall finds invocations of name within src and allows custom
// replacement based on the parsed arguments. The repl function receives the
// original call substring and the slice of parsed arguments and must return the
// replacement text (which may be the original call if no change is needed).
func replaceCall(src, name string, repl func(call string, args []string) string) string {
	re := regexp.MustCompile(regexp.QuoteMeta(name) + `\(`)
	matches := re.FindAllStringIndex(src, -1)
	if len(matches) == 0 {
		return src
	}

	var b strings.Builder
	last := 0
	for _, m := range matches {
		if _, err := b.WriteString(src[last:m[0]]); err != nil {
			return src
		}
		start := m[1]
		end, inner := extractCall(src, start)
		call := src[m[0]:end]
		args := splitArgs(inner)
		replCall := repl(call, args)
		if replCall != call && strings.HasPrefix(replCall, name+"(") {
			if end2, inner2 := extractCall(replCall, len(name)+1); end2 <= len(replCall) {
				args2 := splitArgs(inner2)
				if repl(replCall, args2) != replCall {
					replCall = call
				}
			}
		}
		if _, err := b.WriteString(replCall); err != nil {
			return src
		}
		last = end
	}
	if _, err := b.WriteString(src[last:]); err != nil {
		return src
	}
	return b.String()
}

// identHasType reports whether the identifier appears with the given type in the
// source. The type comparison ignores whether the identifier is a pointer.
func identHasType(src, ident, typ string) bool {
	re := regexp.MustCompile(fmt.Sprintf(`\b%[1]s\s*\*?%[2]s\b`, regexp.QuoteMeta(ident), regexp.QuoteMeta(typ)))
	return re.MatchString(src)
}

// identAssignedFrom reports whether the identifier is assigned from a function
// matching the provided pattern.
func identAssignedFrom(src, ident, fnPattern string) bool {
	re := regexp.MustCompile(fmt.Sprintf(`\b%[1]s\s*:?[=]\s*%[2]s`, regexp.QuoteMeta(ident), fnPattern))
	return re.MatchString(src)
}

// isFiberApp reports whether ident references a Fiber application instance.
func isFiberApp(src, ident string) bool {
	if identHasType(src, ident, "fiber.App") {
		return true
	}
	return identAssignedFrom(src, ident, "fiber\\.New\\(")
}

// isFiberGroup reports whether ident references a Fiber route group.
func isFiberGroup(src, ident string) bool {
	if identHasType(src, ident, "fiber.Group") {
		return true
	}
	return identAssignedFrom(src, ident, "\\w+\\.Group\\(")
}

// isFiberRouter reports whether ident implements a Fiber routing interface or
// is a concrete router such as an App or Group.
func isFiberRouter(src, ident string) bool {
	if identHasType(src, ident, "fiber.Router") || identHasType(src, ident, "fiber.Registering") {
		return true
	}
	return isFiberApp(src, ident) || isFiberGroup(src, ident)
}

// isFiberCtx reports whether ident references a Fiber request context.
func isFiberCtx(src, ident string) bool {
	return identHasType(src, ident, "fiber.Ctx")
}

func addImport(content, path string) string {
	imp := fmt.Sprintf("%q", path)
	if strings.Contains(content, imp) {
		return content
	}

	if idx := strings.Index(content, "import (\n"); idx != -1 {
		idx += len("import (\n")
		return content[:idx] + "\t" + imp + "\n" + content[idx:]
	}

	re := regexp.MustCompile(`(?m)^import\s+[^\(\n]+`)
	if loc := re.FindStringIndex(content); loc != nil {
		existing := strings.TrimSpace(content[loc[0]:loc[1]])
		existing = strings.TrimPrefix(existing, "import")
		block := "import (\n\t" + existing + "\n\t" + imp + "\n)"
		return content[:loc[0]] + block + content[loc[1]:]
	}

	rePkg := regexp.MustCompile(`(?m)^package\s+\w+`)
	if loc := rePkg.FindStringIndex(content); loc != nil {
		insert := loc[1]
		return content[:insert] + "\n\nimport " + imp + "\n" + content[insert:]
	}

	return content
}

// GetBaseIdent recursively resolves an expression to its base identifier.
// It handles selector expressions, call expressions, and identifiers.
// Returns nil if the expression cannot be resolved to a simple identifier.
func GetBaseIdent(expr ast.Expr) *ast.Ident {
	for {
		switch e := expr.(type) {
		case *ast.Ident:
			return e
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.CallExpr:
			expr = e.Fun
		default:
			return nil
		}
	}
}

// ExtractCommentAndValue separates a value from its trailing comment.
// It handles both line comments (//) and block comments (/* */).
// Returns the value (trimmed) and the comment (with original delimiters).
func ExtractCommentAndValue(line string) (value, comment string) {
	value = line
	if idx := strings.Index(line, "//"); idx >= 0 {
		comment = strings.TrimSpace(line[idx:])
		value = strings.TrimSpace(line[:idx])
	} else if idx := strings.Index(line, "/*"); idx >= 0 {
		comment = strings.TrimSpace(line[idx:])
		value = strings.TrimSpace(line[:idx])
	}
	return value, comment
}

// FormatFieldWithComment formats a field assignment with consistent spacing
// for indentation, value, comma, comment, and newline.
func FormatFieldWithComment(indent, fieldName, value, comma, comment, newline string) string {
	if comment != "" {
		comment = " " + comment
	}
	return fmt.Sprintf("%s%s: %s%s%s%s", indent, fieldName, value, comma, comment, newline)
}

// IterateConfigBlocks finds all occurrences matching the given regex pattern,
// extracts their config blocks using braces, processes each block with the
// provided function, and reconstructs the content.
func IterateConfigBlocks(content string, pattern *regexp.Regexp, processor func(string) string) string {
	matches := pattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return content
	}

	var b strings.Builder
	last := 0
	for _, m := range matches {
		if _, err := b.WriteString(content[last:m[0]]); err != nil {
			return content
		}
		start := m[0]
		end := extractBlock(content, m[1], '{', '}')
		cfg := content[start:end]

		// Process the config block
		cfg = processor(cfg)

		if _, err := b.WriteString(cfg); err != nil {
			return content
		}
		last = end
	}
	if _, err := b.WriteString(content[last:]); err != nil {
		return content
	}
	return b.String()
}

// BuildExtractorChain builds an extractor expression from a slice of extractors.
// Returns a single extractor for one element, Chain() for multiple, or empty for none.
func BuildExtractorChain(extractors []string) string {
	switch len(extractors) {
	case 0:
		return ""
	case 1:
		return extractors[0]
	default:
		return fmt.Sprintf("extractors.Chain(%s)", strings.Join(extractors, ", "))
	}
}
