package v3

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	clientBytesWithBodyPattern  = regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*fiber\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\.(Body|BodyString)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.Bytes\(\)`)
	clientBytesPattern          = regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*fiber\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.Bytes\(\)`)
	clientStringWithBodyPattern = regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*fiber\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\.(Body|BodyString)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.String\(\)`)
	clientStringPattern         = regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*fiber\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.String\(\)`)
	clientStructWithBodyPattern = regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*fiber\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\.(Body|BodyString)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.Struct\(([^)]*)\)`)
	clientStructPattern         = regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*fiber\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.Struct\(([^)]*)\)`)
	clientErrIfPattern          = regexp.MustCompile(`if\s+len\(\s*errs\s*\)\s*>\s*0\s*{`)
	clientErrLenPattern         = regexp.MustCompile(`\blen\(errs\)`)
	clientErrComparePattern     = regexp.MustCompile(`err\s*!=\s*nil\s*>\s*0`)
	clientErrMapPattern         = regexp.MustCompile(`"errs"\s*:\s*errs`)
	clientErrVarPattern         = regexp.MustCompile(`\berrs\b`)

	acquireAgentPattern = regexp.MustCompile(`(?m)^([ \t]*)(\w+)\s*:=\s*fiber\.AcquireAgent\(\)\s*$`)
	requestFromAgent    = regexp.MustCompile(`^([ \t]*)(\w+)\s*:=\s*(\w+)\.Request\(\)\s*$`)
	headerMethodPattern = regexp.MustCompile(`^([ \t]*)(\w+)\.Header\.SetMethod\(([^)]*)\)\s*$`)
	headerSetPattern    = regexp.MustCompile(`^([ \t]*)(\w+)\.Header\.Set\(([^,]+),\s*([^)]*)\)\s*$`)
	requestURIPattern   = regexp.MustCompile(`^([ \t]*)(\w+)\.SetRequestURI\((.*)\)\s*$`)
	parseCallPattern    = regexp.MustCompile(`^([ \t]*)if\s+err\s*:=\s*(\w+)\.Parse\(\);\s*err\s*!=\s*nil\s*{\s*$`)
	structAssignPattern = regexp.MustCompile(`^([ \t]*)if\s+(.+?)\s*([:=]?)=\s*(\w+)\.Struct\((.*)\);\s*len\(errs\)\s*>\s*0\s*{\s*$`)
)

func MigrateClientUsage(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		updated, modified := rewriteClientExamples(content)
		updated, agentChanged := rewriteAcquireAgentBlocks(updated)
		modified = modified || agentChanged
		if !modified {
			return content
		}

		updated = rewriteClientErrorHandling(updated)
		return updated
	})
	if err != nil {
		return fmt.Errorf("failed to migrate client usage: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating client usage")
	return nil
}

func rewriteAcquireAgentBlocks(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	var out []string
	changed := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		acquire := acquireAgentPattern.FindStringSubmatch(line)
		if acquire == nil {
			out = append(out, line)
			continue
		}

		indent := acquire[1]
		agentVar := acquire[2]

		if i+1 >= len(lines) {
			out = append(out, line)
			continue
		}

		reqMatch := requestFromAgent.FindStringSubmatch(lines[i+1])
		if len(reqMatch) == 0 || reqMatch[3] != agentVar {
			out = append(out, line)
			continue
		}

		reqVar := reqMatch[2]
		methodExpr := ""
		uriExpr := ""
		headers := make(map[string]string)

		j := i + 2
		for ; j < len(lines); j++ {
			l := lines[j]
			if m := headerMethodPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == reqVar {
				methodExpr = strings.TrimSpace(m[3])
				continue
			}
			if m := headerSetPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == reqVar {
				headers[strings.TrimSpace(m[3])] = strings.TrimSpace(m[4])
				continue
			}
			if m := requestURIPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == reqVar {
				uriExpr = strings.TrimSpace(m[3])
				continue
			}
			if parseCallPattern.MatchString(l) {
				break
			}
		}

		if j >= len(lines) || uriExpr == "" || methodExpr == "" {
			out = append(out, line)
			continue
		}

		parseStart := j
		parseMatch := parseCallPattern.FindStringSubmatch(lines[parseStart])
		if parseMatch == nil {
			out = append(out, line)
			continue
		}
		parseIndent := parseMatch[1]
		parseBody := []string{}
		braceDepth := 0
		for j++; j < len(lines); j++ {
			parseBody = append(parseBody, lines[j])
			braceDepth += strings.Count(lines[j], "{")
			braceDepth -= strings.Count(lines[j], "}")
			if braceDepth < 0 {
				break
			}
		}
		if j >= len(lines) || len(parseBody) == 0 {
			out = append(out, line)
			continue
		}
		parseEnd := j

		structStart := parseEnd + 1
		for structStart < len(lines) && strings.TrimSpace(lines[structStart]) == "" {
			structStart++
		}
		if structStart >= len(lines) {
			out = append(out, line)
			continue
		}

		structMatch := structAssignPattern.FindStringSubmatch(lines[structStart])
		if len(structMatch) == 0 || structMatch[4] != agentVar {
			out = append(out, line)
			continue
		}

		statusVar, bodyVar := "", ""
		assigns := strings.Split(structMatch[2], ",")
		if len(assigns) >= 2 {
			statusVar = strings.TrimSpace(assigns[0])
			bodyVar = strings.TrimSpace(assigns[1])
		}
		assignOp := structMatch[3]
		if assignOp == "" {
			assignOp = "="
		}
		structTarget := strings.TrimSpace(structMatch[5])

		structBody := []string{}
		braceDepth = 0
		for k := structStart + 1; k < len(lines); k++ {
			structBody = append(structBody, lines[k])
			braceDepth += strings.Count(lines[k], "{")
			braceDepth -= strings.Count(lines[k], "}")
			if braceDepth < 0 {
				structStart = k
				break
			}
		}
		if len(structBody) == 0 {
			out = append(out, line)
			continue
		}

		methodName := methodFromExpr(methodExpr)
		configLine := buildConfig(headers)
		respLine := fmt.Sprintf("%sresp, err := client.%s(%s%s)", indent, methodName, uriExpr, configLine)

		out = append(out, respLine)
		out = append(out, parseIndent+"if err != nil {")
		out = append(out, parseBody[:len(parseBody)-1]...)
		out = append(out, parseIndent+"}")

		if statusVar != "" {
			out = append(out, fmt.Sprintf("%s%s %s resp.StatusCode()", indent, statusVar, assignOp))
		}
		if bodyVar != "" {
			out = append(out, fmt.Sprintf("%s%s %s resp.Body()", indent, bodyVar, assignOp))
		}
		out = append(out, indent+"if err == nil {")
		out = append(out, fmt.Sprintf("%s\terr = resp.JSON(%s)", indent, structTarget))
		out = append(out, indent+"}")
		out = append(out, structMatch[1]+"if err != nil {")
		out = append(out, structBody[:len(structBody)-1]...)
		out = append(out, structMatch[1]+"}")

		i = structStart
		changed = true
		continue
	}

	if changed {
		content = ensureClientImport(strings.Join(out, "\n"))
		return content, true
	}

	return strings.Join(out, "\n"), false
}

func methodFromExpr(expr string) string {
	lower := strings.ToLower(expr)
	switch {
	case strings.Contains(lower, "methodpost"):
		return "Post"
	case strings.Contains(lower, "methodput"):
		return "Put"
	case strings.Contains(lower, "methodpatch"):
		return "Patch"
	case strings.Contains(lower, "methoddelete"):
		return "Delete"
	case strings.Contains(lower, "methodhead"):
		return "Head"
	default:
		return "Get"
	}
}

func buildConfig(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	var keys []string
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, headers[k]))
	}
	return fmt.Sprintf(", client.Config{Header: map[string]string{%s}}", strings.Join(parts, ", "))
}

func rewriteClientExamples(content string) (string, bool) {
	updated := content
	changed := false

	for _, replace := range []struct {
		pattern *regexp.Regexp
		build   func(parts []string) (string, bool)
	}{
		{pattern: clientBytesWithBodyPattern, build: buildBytesWithBodyReplacement},
		{pattern: clientBytesPattern, build: buildBytesReplacement},
		{pattern: clientStringWithBodyPattern, build: buildStringWithBodyReplacement},
		{pattern: clientStringPattern, build: buildStringReplacement},
		{pattern: clientStructWithBodyPattern, build: buildStructWithBodyReplacement},
		{pattern: clientStructPattern, build: buildStructReplacement},
	} {
		updated = replace.pattern.ReplaceAllStringFunc(updated, func(match string) string {
			parts := replace.pattern.FindStringSubmatch(match)
			repl, ok := replace.build(parts)
			if !ok {
				return match
			}
			changed = true
			return repl
		})
	}

	if changed {
		updated = ensureClientImport(updated)
	}

	return updated, changed
}

func buildBytesWithBodyReplacement(parts []string) (string, bool) {
	if len(parts) != 13 {
		return "", false
	}

	indent, varInit, method, urlArg := parts[1], parts[2], parts[3], strings.TrimSpace(parts[4])
	varBody, bodyArg, callIndent, statusVar, bodyVar, varBytes := parts[6], strings.TrimSpace(parts[8]), parts[9], parts[10], parts[11], parts[12]
	if varInit != varBody || varInit != varBytes {
		return "", false
	}

	return fmt.Sprintf("%s%s, err := client.%s(%s, client.Config{Body: %s})\n%s%s := %s.StatusCode()\n%s%s := %s.Body()", indent, varInit, method, urlArg, bodyArg, callIndent, statusVar, varInit, callIndent, bodyVar, varInit), true
}

func buildBytesReplacement(parts []string) (string, bool) {
	if len(parts) != 9 {
		return "", false
	}

	indent, varInit, method, urlArg := parts[1], parts[2], parts[3], strings.TrimSpace(parts[4])
	callIndent, statusVar, bodyVar, varBytes := parts[5], parts[6], parts[7], parts[8]
	if varInit != varBytes {
		return "", false
	}

	return fmt.Sprintf("%s%s, err := client.%s(%s)\n%s%s := %s.StatusCode()\n%s%s := %s.Body()", indent, varInit, method, urlArg, callIndent, statusVar, varInit, callIndent, bodyVar, varInit), true
}

func buildStringWithBodyReplacement(parts []string) (string, bool) {
	if len(parts) != 13 {
		return "", false
	}

	indent, varInit, method, urlArg := parts[1], parts[2], parts[3], strings.TrimSpace(parts[4])
	varBody, bodyArg, callIndent, statusVar, bodyVar, varBytes := parts[6], strings.TrimSpace(parts[8]), parts[9], parts[10], parts[11], parts[12]
	if varInit != varBody || varInit != varBytes {
		return "", false
	}

	return fmt.Sprintf("%s%s, err := client.%s(%s, client.Config{Body: %s})\n%s%s := %s.StatusCode()\n%s%s := %s.String()", indent, varInit, method, urlArg, bodyArg, callIndent, statusVar, varInit, callIndent, bodyVar, varInit), true
}

func buildStringReplacement(parts []string) (string, bool) {
	if len(parts) != 9 {
		return "", false
	}

	indent, varInit, method, urlArg := parts[1], parts[2], parts[3], strings.TrimSpace(parts[4])
	callIndent, statusVar, bodyVar, varBytes := parts[5], parts[6], parts[7], parts[8]
	if varInit != varBytes {
		return "", false
	}

	return fmt.Sprintf("%s%s, err := client.%s(%s)\n%s%s := %s.StatusCode()\n%s%s := %s.String()", indent, varInit, method, urlArg, callIndent, statusVar, varInit, callIndent, bodyVar, varInit), true
}

func buildStructWithBodyReplacement(parts []string) (string, bool) {
	if len(parts) != 14 {
		return "", false
	}

	indent, varInit, method, urlArg := parts[1], parts[2], parts[3], strings.TrimSpace(parts[4])
	varBody, bodyArg, callIndent, statusVar, bodyVar, varBytes, target := parts[6], strings.TrimSpace(parts[8]), parts[9], parts[10], parts[11], parts[12], strings.TrimSpace(parts[13])
	if varInit != varBody || varInit != varBytes {
		return "", false
	}

	return fmt.Sprintf("%s%s, err := client.%s(%s, client.Config{Body: %s})\n%s%s := %s.StatusCode()\n%s%s := %s.Body()\n%sif err == nil {\n%s\terr = %s.JSON(%s)\n%s}", indent, varInit, method, urlArg, bodyArg, callIndent, statusVar, varInit, callIndent, bodyVar, varInit, callIndent, callIndent, varInit, target, callIndent), true
}

func buildStructReplacement(parts []string) (string, bool) {
	if len(parts) != 10 {
		return "", false
	}

	indent, varInit, method, urlArg := parts[1], parts[2], parts[3], strings.TrimSpace(parts[4])
	callIndent, statusVar, bodyVar, varBytes, target := parts[5], parts[6], parts[7], parts[8], strings.TrimSpace(parts[9])
	if varInit != varBytes {
		return "", false
	}

	return fmt.Sprintf("%s%s, err := client.%s(%s)\n%s%s := %s.StatusCode()\n%s%s := %s.Body()\n%sif err == nil {\n%s\terr = %s.JSON(%s)\n%s}", indent, varInit, method, urlArg, callIndent, statusVar, varInit, callIndent, bodyVar, varInit, callIndent, callIndent, varInit, target, callIndent), true
}

func ensureClientImport(content string) string {
	if strings.Contains(content, "\"github.com/gofiber/fiber/v3/client\"") {
		return content
	}

	packageRegex := regexp.MustCompile(`(?m)^package\s+\w+`)
	if match := packageRegex.FindString(content); match != "" {
		return strings.Replace(content, match, match+"\n\nimport \"github.com/gofiber/fiber/v3/client\"", 1)
	}

	return "import \"github.com/gofiber/fiber/v3/client\"\n" + content
}

func rewriteClientErrorHandling(content string) string {
	updated := clientErrIfPattern.ReplaceAllString(content, "if err != nil {")
	updated = clientErrLenPattern.ReplaceAllString(updated, "err != nil")
	updated = clientErrComparePattern.ReplaceAllString(updated, "err != nil")
	updated = clientErrMapPattern.ReplaceAllString(updated, `"err": err`)
	updated = clientErrVarPattern.ReplaceAllString(updated, "err")
	return updated
}
