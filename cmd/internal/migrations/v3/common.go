package v3

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	hexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	b64Re = regexp.MustCompile(`^[A-Za-z0-9+/]{43}=?$`)
)

// skipCommaSuffix advances the index past a comma and any trailing
// whitespace or newline characters.
func skipCommaSuffix(src string, i int) int {
	i++
	for i < len(src) {
		switch src[i] {
		case ' ', '\t':
			i++
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
	re := regexp.MustCompile(`(?m)^\s*` + field + `:\s*`)
	for {
		loc := re.FindStringIndex(src)
		if loc == nil {
			break
		}
		start := loc[0]
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
			} else {
				switch ch {
				case '"':
					inString = true
				case '(', '{', '[':
					depth++
				case ')', '}', ']':
					if depth > 0 {
						depth--
					}
				case ',':
					if depth == 0 {
						i = skipCommaSuffix(src, i)
						src = src[:start] + src[i:]
						goto nextField
					}
				case '\n':
					if depth == 0 {
						i++
						src = src[:start] + src[i:]
						goto nextField
					}
				}
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
	re := regexp.MustCompile(`(?m)(\s*)KeyLookup:\s*([^\n]+)(\n?)`)
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

		if uq, err := strconv.Unquote(val); err == nil {
			val = uq
			repl := fn(indent, val, comma, comment, newline)
			if repl == "" {
				if comment != "" {
					return fmt.Sprintf("%s%s%s", indent, comment, newline)
				}
				return newline
			}
			return repl
		}

		if comment != "" {
			return fmt.Sprintf("%s// TODO: migrate KeyLookup: %s %s%s", indent, val, comment, newline)
		}
		return fmt.Sprintf("%s// TODO: migrate KeyLookup: %s%s", indent, val, newline)
	})
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
		}
	}
	return len(src), src[start:]
}

// extractBlock returns the index just after the closing delimiter that matches
// the opening delimiter at the start position. Start must point to the first
// character after the opening delimiter. It handles nested delimiters and
// quoted strings.
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
