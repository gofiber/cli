package v3

import (
	"fmt"
	"regexp"
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
		if _, err := b.WriteString(repl(call, args)); err != nil {
			return src
		}
		last = end
	}
	if _, err := b.WriteString(src[last:]); err != nil {
		return src
	}
	return b.String()
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
