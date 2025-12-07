package v3

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	// Error handling patterns
	clientErrIfPattern      = regexp.MustCompile(`if\s+len\(\s*errs\s*\)\s*>\s*0\s*{`)
	clientErrLenPattern     = regexp.MustCompile(`\blen\(errs\)`)
	clientErrComparePattern = regexp.MustCompile(`err\s*!=\s*nil\s*>\s*0`)
	clientErrMapPattern     = regexp.MustCompile(`"errs"\s*:\s*errs`)
	clientErrsDeclPattern   = regexp.MustCompile(`(?m)^\s*var\s+errs\b`)

	// Non-alias-dependent patterns
	requestFromAgent    = regexp.MustCompile(`^([ \t]*)(\w+)\s*:=\s*(\w+)\.Request\(\)\s*$`)
	headerMethodPattern = regexp.MustCompile(`^([ \t]*)(\w+)\.Header\.SetMethod\(([^)]*)\)\s*$`)
	headerSetPattern    = regexp.MustCompile(`^([ \t]*)(\w+)\.Header\.Set\(([^,]+),\s*([^)]*)\)\s*$`)
	requestURIPattern   = regexp.MustCompile(`^([ \t]*)(\w+)\.SetRequestURI\((.*)\)\s*$`)
	parseCallPattern    = regexp.MustCompile(`^([ \t]*)if\s+err\s*:=\s*(\w+)\.Parse\(\);\s*err\s*!=\s*nil\s*{\s*$`)
	requestBodyPattern  = regexp.MustCompile(`^([ \t]*)(\w+)\.(SetBody(?:String)?)\((.*)\)\s*$`)
	structAssignPattern = regexp.MustCompile(`^([ \t]*)if\s+([^,]+?)\s*,\s*([^,]+?)\s*,\s*errs\s*([:=]?)=\s*(\w+)\.Struct\((.*)\);\s*len\(errs\)\s*>\s*0\s*{\s*$`)
	bytesAssignPattern  = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.Bytes\(\)\s*$`)
	stringAssignPattern = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.String\(\)\s*$`)
)

var (
	// Agent method patterns (non-alias-dependent)
	headerSetSimplePattern    = regexp.MustCompile(`^([ \t]*)(\w+)\.Set\(([^,]+),\s*([^)]*)\)\s*$`)
	queryStringPattern        = regexp.MustCompile(`^([ \t]*)(\w+)\.QueryString\(([^)]*)\)\s*$`)
	timeoutPattern            = regexp.MustCompile(`^([ \t]*)(\w+)\.Timeout\(([^)]*)\)\s*$`)
	jsonBodyPattern           = regexp.MustCompile(`^([ \t]*)(\w+)\.JSON\(([^)]*)\)\s*$`)
	bodyPattern               = regexp.MustCompile(`^([ \t]*)(\w+)\.(Body|BodyString)\(([^)]*)\)\s*$`)
	basicAuthPattern          = regexp.MustCompile(`^([ \t]*)(\w+)\.BasicAuth\(([^,]+),\s*([^)]*)\)\s*$`)
	tlsConfigPattern          = regexp.MustCompile(`^([ \t]*)(\w+)\.TLSConfig\(([^)]*)\)\s*$`)
	debugPattern              = regexp.MustCompile(`^([ \t]*)(\w+)\.Debug\(([^)]*)\)\s*$`)
	reusePattern              = regexp.MustCompile(`^([ \t]*)(\w+)\.Reuse\(\)\s*$`)
	agentBytesCallPattern     = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.Bytes\(\)\s*$`)
	agentStringCallPattern    = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.String\(\)\s*$`)
	agentStructCallPattern    = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.Struct\((.+)\)\s*$`)
	futureErrConflictPattern  = regexp.MustCompile(`\b(?:var\s+err\b|err\s*:=)`)   // detects future err declarations that would clash with injected err
	futureErrsConflictPattern = regexp.MustCompile(`\b(?:var\s+errs\b|errs\s*:=)`) // errs will be renamed to err during rewrite
)

// buildAliasPatterns creates regex patterns for fiber package calls with given alias
func buildAliasPatterns(alias string) map[string]*regexp.Regexp {
	escaped := regexp.QuoteMeta(alias)
	return map[string]*regexp.Regexp{
		"simpleAgent":    regexp.MustCompile(`^([ \t]*)(\w+)\s*:=\s*` + escaped + `\.(Get|Head|Post|Put|Patch|Delete)\((.*)\)\s*$`),
		"acquireAgent":   regexp.MustCompile(`(?m)^([ \t]*)(\w+)\s*:=\s*` + escaped + `\.AcquireAgent\(\)\s*$`),
		"releaseAgent":   regexp.MustCompile(`(?m)^([ \t]*)defer\s+` + escaped + `\.ReleaseAgent\((\w+)\)\s*$`),
		"bytesWithBody":  regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*` + escaped + `\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\.(Body|BodyString)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.Bytes\(\)`),
		"bytes":          regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*` + escaped + `\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.Bytes\(\)`),
		"stringWithBody": regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*` + escaped + `\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\.(Body|BodyString)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.String\(\)`),
		"string":         regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*` + escaped + `\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.String\(\)`),
		"structWithBody": regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*` + escaped + `\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\.(Body|BodyString)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.Struct\(([^)]*)\)`),
		"struct":         regexp.MustCompile(`(?m)([ \t]*)(\w+)\s*:=\s*` + escaped + `\.(Get|Head|Post|Put|Patch|Delete)\(([^)]*)\)\s*\n([ \t]*)(\w+)\s*,\s*(\w+)\s*,\s*errs\s*:=\s*(\w+)\.Struct\(([^)]*)\)`),
	}
}

const (
	callTypeString = "string"
	defaultErrName = "err"
)

func MigrateClientUsage(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		// Find all fiber import aliases used in this file
		aliases := findFiberImportAliases(content)
		if len(aliases) == 0 {
			// Default to "fiber" if no import found (might be in another file)
			aliases = []string{"fiber"}
		}

		modified := false
		updated := content

		// Apply migrations for each alias
		for _, alias := range aliases {
			var changed bool
			updated, changed = rewriteClientExamplesWithAlias(updated, alias)
			modified = modified || changed
			updated, changed = rewriteAcquireAgentBlocksWithAlias(updated, alias)
			modified = modified || changed
		}

		if !modified {
			return content
		}

		updated = rewriteClientErrorHandling(updated)
		updated = removeUnusedFiberImport(updated)
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

func rewriteAcquireAgentBlocksWithAlias(content, alias string) (string, bool) {
	patterns := buildAliasPatterns(alias)
	acquireAgentPattern := patterns["acquireAgent"]
	releaseAgentPattern := patterns["releaseAgent"]

	lines := strings.Split(content, "\n")
	var out []string
	changed := false
	skipLines := make(map[int]bool)

	// First pass: mark ReleaseAgent defer lines for removal
	for i, line := range lines {
		if releaseAgentPattern.MatchString(line) {
			skipLines[i] = true
		}
	}

	for i := 0; i < len(lines); i++ {
		if skipLines[i] {
			changed = true
			continue
		}

		line := lines[i]
		acquire := acquireAgentPattern.FindStringSubmatch(line)
		if acquire == nil {
			out = append(out, line)
			continue
		}

		indent := acquire[1]
		agentVar := acquire[2]

		reqLine := -1
		var reqMatch []string
		for j := i + 1; j < len(lines); j++ {
			if skipLines[j] {
				continue
			}
			trimmed := strings.TrimSpace(lines[j])
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}

			if m := requestFromAgent.FindStringSubmatch(lines[j]); len(m) > 0 && m[3] == agentVar {
				reqMatch = m
				reqLine = j
			}
			break
		}

		if reqLine == -1 {
			out = append(out, line)
			continue
		}

		reqVar := reqMatch[2]
		methodExpr := ""
		uriExpr := ""
		headers := make(map[string]string)
		bodyExpr := ""
		bodyIsString := false

		j := reqLine + 1
		for ; j < len(lines); j++ {
			if skipLines[j] {
				continue
			}
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
			if m := requestBodyPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == reqVar {
				bodyExpr = strings.TrimSpace(m[4])
				bodyIsString = m[3] == "SetBodyString"
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

		structStart := -1
		preservedLines := []string{}
		preservedIdx := []int{}
		blankIdx := []int{}
		blankAfterParse := false
		for k := parseEnd + 1; k < len(lines); k++ {
			if skipLines[k] {
				continue
			}

			trimmed := strings.TrimSpace(lines[k])
			if trimmed == "" {
				blankAfterParse = true
				blankIdx = append(blankIdx, k)
				continue
			}
			if strings.HasPrefix(trimmed, "//") {
				preservedLines = append(preservedLines, lines[k])
				preservedIdx = append(preservedIdx, k)
				continue
			}

			if strings.HasPrefix(trimmed, "var ") {
				fields := strings.Fields(trimmed)
				if len(fields) >= 2 {
					name := fields[1]
					if name == defaultErrName || name == "errs" {
						continue
					}
				}

				preservedLines = append(preservedLines, lines[k])
				preservedIdx = append(preservedIdx, k)
				continue
			}

			structStart = k
			break
		}

		if structStart == -1 {
			structStart = parseEnd
		}

		structMatch := structAssignPattern.FindStringSubmatch(lines[structStart])
		bytesMatch := bytesAssignPattern.FindStringSubmatch(lines[structStart])
		stringMatch := stringAssignPattern.FindStringSubmatch(lines[structStart])
		parseOnly := len(structMatch) == 0 && len(bytesMatch) == 0 && len(stringMatch) == 0

		addPreserved := func() {
			if len(preservedLines) == 0 {
				return
			}
			if blankAfterParse && parseOnly {
				out = append(out, "")
				for _, idx := range blankIdx {
					skipLines[idx] = true
				}
				blankAfterParse = false
			}

			out = append(out, preservedLines...)
			for _, idx := range preservedIdx {
				skipLines[idx] = true
			}
		}

		if len(preservedLines) == 0 && !parseOnly {
			for _, idx := range blankIdx {
				skipLines[idx] = true
			}
			blankAfterParse = false
		}

		if len(structMatch) == 0 && len(bytesMatch) == 0 && len(stringMatch) == 0 {
			structStart = parseEnd
		}

		errName := chooseErrName(out, lines, i)
		errBlockEnd := blockEndIndex(lines, structStart)
		if errBlockEnd < structStart {
			errBlockEnd = structStart
		}
		errsNeeded := identifierUsedAfter(lines[errBlockEnd+1:], "errs")
		errsAlreadyDeclared := identifierDeclared(out, lines, i, "errs") || identifierDeclaredInLines(preservedLines, "errs")

		out = append(out, fmt.Sprintf("%s%s := client.New()", indent, agentVar))
		out = append(out, fmt.Sprintf("%s%s := %s.R()", indent, reqVar, agentVar))
		if methodExpr != "" {
			out = append(out, fmt.Sprintf("%s%s.SetMethod(%s)", indent, reqVar, methodLiteral(methodExpr)))
		}
		if uriExpr != "" {
			out = append(out, fmt.Sprintf("%s%s.SetURL(%s)", indent, reqVar, uriExpr))
		}

		if len(headers) > 0 {
			keys := make([]string, 0, len(headers))
			for k := range headers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				out = append(out, fmt.Sprintf("%s%s.SetHeader(%s, %s)", indent, reqVar, k, headers[k]))
			}
		}

		if bodyExpr != "" {
			rawBody := bodyExpr
			if bodyIsString {
				rawBody = fmt.Sprintf("[]byte(%s)", bodyExpr)
			}
			out = append(out, fmt.Sprintf("%s%s.SetRawBody(%s)", indent, reqVar, rawBody))
		}

		switch {
		case len(structMatch) > 0 && structMatch[5] == agentVar:
			addPreserved()
			errAssign := errAssignmentOperator(errName, out, lines, i, "resp")
			statusVar := strings.TrimSpace(structMatch[2])
			bodyVar := strings.TrimSpace(structMatch[3])
			structTarget := strings.TrimSpace(structMatch[6])
			statusDeclared := statusVar != "" && (identifierDeclaredInLines(preservedLines, statusVar) || identifierDeclared(out, lines, i, statusVar))
			bodyDeclared := bodyVar != "" && (identifierDeclaredInLines(preservedLines, bodyVar) || identifierDeclared(out, lines, i, bodyVar))

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

			respLine := fmt.Sprintf("%sresp, %s %s %s.Send()", indent, errName, errAssign, reqVar)

			out = append(out, respLine)
			out = append(out, fmt.Sprintf("%sif %s != nil {", parseIndent, errName))
			out = append(out, replaceErrIdentifier(parseBody[:len(parseBody)-1], errName, false)...)
			out = append(out, parseIndent+"}")
			// Declare variables and assign only if err == nil to avoid nil pointer dereference
			if statusVar != "" && !statusDeclared {
				out = append(out, fmt.Sprintf("%svar %s int", indent, statusVar))
			}
			if bodyVar != "" && !bodyDeclared {
				out = append(out, fmt.Sprintf("%svar %s []byte", indent, bodyVar))
			}
			out = append(out, fmt.Sprintf("%sif %s == nil {", indent, errName))
			if statusVar != "" {
				out = append(out, fmt.Sprintf("%s\t%s = resp.StatusCode()", indent, statusVar))
			}
			if bodyVar != "" {
				out = append(out, fmt.Sprintf("%s\t%s = resp.Body()", indent, bodyVar))
			}
			out = append(out, fmt.Sprintf("%s\t%s = resp.JSON(%s)", indent, errName, structTarget))
			out = append(out, indent+"}")
			out = append(out, fmt.Sprintf("%sif %s != nil {", structMatch[1], errName))
			out = append(out, replaceErrIdentifier(structBody[:len(structBody)-1], errName, true)...)
			out = append(out, structMatch[1]+"}")
			if errsNeeded && !errsAlreadyDeclared {
				out = append(out, indent+"var errs []error")
			}
			if errsNeeded {
				out = append(out, fmt.Sprintf("%sif %s != nil {", indent, errName))
				out = append(out, fmt.Sprintf("%s\terrs = append(errs, %s)", indent, errName))
				out = append(out, indent+"}")
			}
			if statusVar != "" && !hasBlankAssignment(lines, statusVar) && !identifierUsedAfter(lines[structStart+1:], statusVar) {
				out = append(out, fmt.Sprintf("%s_ = %s", indent, statusVar))
			}
			if bodyVar != "" && !hasBlankAssignment(lines, bodyVar) && !identifierUsedAfter(lines[structStart+1:], bodyVar) {
				out = append(out, fmt.Sprintf("%s_ = %s", indent, bodyVar))
			}

			i = structStart
			changed = true
			continue
		case len(bytesMatch) > 0 && bytesMatch[5] == agentVar:
			addPreserved()
			errAssign := errAssignmentOperator(errName, out, lines, i, "resp")
			statusVar := strings.TrimSpace(bytesMatch[2])
			bodyVar := strings.TrimSpace(bytesMatch[3])
			statusDeclared := statusVar != "" && (identifierDeclaredInLines(preservedLines, statusVar) || identifierDeclared(out, lines, i, statusVar))
			bodyDeclared := bodyVar != "" && (identifierDeclaredInLines(preservedLines, bodyVar) || identifierDeclared(out, lines, i, bodyVar))

			respLine := fmt.Sprintf("%sresp, %s %s %s.Send()", indent, errName, errAssign, reqVar)
			out = append(out, respLine)
			out = append(out, fmt.Sprintf("%sif %s != nil {", parseIndent, errName))
			out = append(out, replaceErrIdentifier(parseBody[:len(parseBody)-1], errName, false)...)
			out = append(out, parseIndent+"}")
			// Declare variables and assign only if err == nil to avoid nil pointer dereference
			if statusVar != "" && !statusDeclared {
				out = append(out, fmt.Sprintf("%svar %s int", indent, statusVar))
			}
			if bodyVar != "" && !bodyDeclared {
				out = append(out, fmt.Sprintf("%svar %s []byte", indent, bodyVar))
			}
			out = append(out, fmt.Sprintf("%sif %s == nil {", indent, errName))
			out = append(out, fmt.Sprintf("%s\t%s = resp.StatusCode()", indent, statusVar))
			out = append(out, fmt.Sprintf("%s\t%s = resp.Body()", indent, bodyVar))
			out = append(out, indent+"}")
			if errsNeeded && !errsAlreadyDeclared {
				out = append(out, indent+"var errs []error")
			}
			if errsNeeded {
				out = append(out, fmt.Sprintf("%sif %s != nil {", indent, errName))
				out = append(out, fmt.Sprintf("%s\terrs = append(errs, %s)", indent, errName))
				out = append(out, indent+"}")
			}
			if statusVar != "" && !hasBlankAssignment(lines, statusVar) && !identifierUsedAfter(lines[structStart+1:], statusVar) {
				out = append(out, fmt.Sprintf("%s_ = %s", indent, statusVar))
			}
			if bodyVar != "" && !hasBlankAssignment(lines, bodyVar) && !identifierUsedAfter(lines[structStart+1:], bodyVar) {
				out = append(out, fmt.Sprintf("%s_ = %s", indent, bodyVar))
			}

			i = structStart
			changed = true
			continue
		case len(stringMatch) > 0 && stringMatch[5] == agentVar:
			addPreserved()
			errAssign := errAssignmentOperator(errName, out, lines, i, "resp")
			statusVar := strings.TrimSpace(stringMatch[2])
			bodyVar := strings.TrimSpace(stringMatch[3])
			statusDeclared := statusVar != "" && (identifierDeclaredInLines(preservedLines, statusVar) || identifierDeclared(out, lines, i, statusVar))
			bodyDeclared := bodyVar != "" && (identifierDeclaredInLines(preservedLines, bodyVar) || identifierDeclared(out, lines, i, bodyVar))

			respLine := fmt.Sprintf("%sresp, %s %s %s.Send()", indent, errName, errAssign, reqVar)
			out = append(out, respLine)
			out = append(out, fmt.Sprintf("%sif %s != nil {", parseIndent, errName))
			out = append(out, replaceErrIdentifier(parseBody[:len(parseBody)-1], errName, false)...)
			out = append(out, parseIndent+"}")
			// Declare variables and assign only if err == nil to avoid nil pointer dereference
			if statusVar != "" && !statusDeclared {
				out = append(out, fmt.Sprintf("%svar %s int", indent, statusVar))
			}
			if bodyVar != "" && !bodyDeclared {
				out = append(out, fmt.Sprintf("%svar %s string", indent, bodyVar))
			}
			out = append(out, fmt.Sprintf("%sif %s == nil {", indent, errName))
			out = append(out, fmt.Sprintf("%s\t%s = resp.StatusCode()", indent, statusVar))
			out = append(out, fmt.Sprintf("%s\t%s = resp.String()", indent, bodyVar))
			out = append(out, indent+"}")
			if errsNeeded && !errsAlreadyDeclared {
				out = append(out, indent+"var errs []error")
			}
			if errsNeeded {
				out = append(out, fmt.Sprintf("%sif %s != nil {", indent, errName))
				out = append(out, fmt.Sprintf("%s\terrs = append(errs, %s)", indent, errName))
				out = append(out, indent+"}")
			}
			if statusVar != "" && !hasBlankAssignment(lines, statusVar) && !identifierUsedAfter(lines[structStart+1:], statusVar) {
				out = append(out, fmt.Sprintf("%s_ = %s", indent, statusVar))
			}
			if bodyVar != "" && !hasBlankAssignment(lines, bodyVar) && !identifierUsedAfter(lines[structStart+1:], bodyVar) {
				out = append(out, fmt.Sprintf("%s_ = %s", indent, bodyVar))
			}

			i = structStart
			changed = true
			continue
		default:
			if !parseOnly {
				addPreserved()
			}
			errAssign := errAssignmentOperator(errName, out, lines, i)
			respLine := fmt.Sprintf("%s_, %s %s %s.Send()", indent, errName, errAssign, reqVar)
			out = append(out, respLine)
			out = append(out, fmt.Sprintf("%sif %s != nil {", parseIndent, errName))
			out = append(out, replaceErrIdentifier(parseBody[:len(parseBody)-1], errName, false)...)
			out = append(out, parseIndent+"}")
			if parseOnly {
				addPreserved()
			}

			i = parseEnd
			changed = true
			continue
		}
	}

	if changed {
		content = ensureClientImport(strings.Join(out, "\n"))
		return content, true
	}

	return strings.Join(out, "\n"), false
}

func errAssignmentOperator(errName string, existing, original []string, idx int, newVars ...string) string {
	errInScope := identifierDeclared(existing, original, idx, errName)
	hasNewVar := false

	for _, name := range newVars {
		if name == "" || name == "_" {
			continue
		}

		if identifierDeclared(existing, original, idx, name) {
			continue
		}

		hasNewVar = true
		break
	}

	if errInScope && !hasNewVar {
		return "="
	}

	return ":="
}

func chooseErrName(existing, original []string, idx int) string {
	if identifierDeclared(existing, original, idx, defaultErrName) {
		return defaultErrName
	}

	if !futureErrConflictExists(original[idx:]) {
		return defaultErrName
	}

	candidates := []string{"clientErr", "agentErr", "parseErr"}
	for i := 0; ; i++ {
		if i >= len(candidates) {
			candidates = append(candidates, fmt.Sprintf("err%d", i-len(candidates)+1))
		}

		name := candidates[i]
		if identifierDeclared(existing, original, idx, name) || identifierDeclaredInLines(original[idx:], name) {
			continue
		}

		return name
	}
}

func futureErrConflictExists(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		if declaredInVarBlockLine(defaultErrName, trimmed) || declaredInVarBlockLine("errs", trimmed) {
			return true
		}

		if parseCallPattern.MatchString(trimmed) {
			continue
		}

		if strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "for ") || strings.HasPrefix(trimmed, "switch ") || strings.HasPrefix(trimmed, "else if ") {
			continue
		}

		if agentBytesCallPattern.MatchString(trimmed) || agentStringCallPattern.MatchString(trimmed) || agentStructCallPattern.MatchString(trimmed) {
			continue
		}

		if futureErrConflictPattern.MatchString(trimmed) || futureErrsConflictPattern.MatchString(trimmed) {
			return true
		}
	}

	return false
}

func replaceErrIdentifier(lines []string, errName string, replaceErrs bool) []string {
	replaced := make([]string, len(lines))
	errPattern := regexp.MustCompile(`\berr\b`)
	errsPattern := regexp.MustCompile(`\berrs\b`)

	for i, line := range lines {
		updated := errPattern.ReplaceAllString(line, errName)
		if replaceErrs {
			replaced[i] = errsPattern.ReplaceAllString(updated, errName)
			continue
		}

		replaced[i] = updated
	}

	return replaced
}

func hasBlankAssignment(lines []string, name string) bool {
	if name == "" {
		return false
	}

	pattern := regexp.MustCompile(fmt.Sprintf(`\b_\s*=\s*%s\b`, regexp.QuoteMeta(name)))

	for _, line := range lines {
		if pattern.MatchString(strings.TrimSpace(line)) {
			return true
		}
	}

	return false
}

func identifierUsedAfter(lines []string, name string) bool {
	if name == "" {
		return false
	}

	pattern := regexp.MustCompile(fmt.Sprintf(`\b%s\b`, regexp.QuoteMeta(name)))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		if pattern.MatchString(trimmed) {
			return true
		}
	}

	return false
}

func blockEndIndex(lines []string, start int) int {
	depth := 0

	for i := start + 1; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{")
		depth -= strings.Count(lines[i], "}")
		if depth < 0 {
			return i
		}
	}

	return len(lines) - 1
}

func identifierDeclared(existing, original []string, idx int, name string) bool {
	if identifierDeclaredInLines(existing, name) {
		return true
	}

	return identifierDeclaredInLines(original[:idx], name)
}

func identifierDeclaredInLines(lines []string, name string) bool {
	if len(lines) == 0 {
		return false
	}

	shortPattern := regexp.MustCompile(fmt.Sprintf(`(^|[\s\(\),])%s\s*:=`, regexp.QuoteMeta(name)))
	varPattern := regexp.MustCompile(fmt.Sprintf(`^var\s+%s\b`, regexp.QuoteMeta(name)))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Declarations inside control statement initializers (e.g.,
		// "if err := ...") are scoped to that statement and should not be
		// treated as function-scope declarations.
		if strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "for ") || strings.HasPrefix(trimmed, "switch ") || strings.HasPrefix(trimmed, "else if ") {
			if declaredInVarBlockLine(name, trimmed) {
				return true
			}

			if name == defaultErrName && declaredInVarBlockLine("errs", trimmed) {
				return true
			}

			continue
		}

		if shortPattern.MatchString(trimmed) || varPattern.MatchString(trimmed) {
			return true
		}

		if declaredInVarBlockLine(name, trimmed) {
			return true
		}

		if name == defaultErrName && declaredInVarBlockLine("errs", trimmed) {
			return true
		}
	}

	return false
}

func declaredInVarBlockLine(name, trimmed string) bool {
	fields := strings.Fields(trimmed)
	if len(fields) < 2 {
		return false
	}
	if fields[0] != name {
		return false
	}

	for _, f := range fields[1:] {
		if strings.Contains(f, ":=") || strings.Contains(f, "=") {
			return false
		}
	}

	return true
}

func methodLiteral(expr string) string {
	lower := strings.ToLower(expr)
	switch {
	case strings.Contains(lower, "methodget"):
		return "\"GET\""
	case strings.Contains(lower, "methodpost"):
		return "\"POST\""
	case strings.Contains(lower, "methodput"):
		return "\"PUT\""
	case strings.Contains(lower, "methodpatch"):
		return "\"PATCH\""
	case strings.Contains(lower, "methoddelete"):
		return "\"DELETE\""
	case strings.Contains(lower, "methodhead"):
		return "\"HEAD\""
	default:
		return expr
	}
}

func rewriteClientExamplesWithAlias(content, alias string) (string, bool) {
	patterns := buildAliasPatterns(alias)

	updated, changedSimple := rewriteSimpleAgentBlocksWithAlias(content, alias)
	changed := changedSimple

	for _, replace := range []struct {
		pattern *regexp.Regexp
		build   func(parts []string) (string, bool)
	}{
		{pattern: patterns["bytesWithBody"], build: buildBytesWithBodyReplacement},
		{pattern: patterns["bytes"], build: buildBytesReplacement},
		{pattern: patterns["stringWithBody"], build: buildStringWithBodyReplacement},
		{pattern: patterns["string"], build: buildStringReplacement},
		{pattern: patterns["structWithBody"], build: buildStructWithBodyReplacement},
		{pattern: patterns["struct"], build: buildStructReplacement},
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

type simpleAgentConfig struct {
	headers   map[string]headerValue
	params    map[string]string
	body      string
	timeout   string
	tlsConfig string
	config    bool
}

type headerValue struct {
	value string
	raw   bool
}

func rewriteSimpleAgentBlocksWithAlias(content, alias string) (string, bool) {
	patterns := buildAliasPatterns(alias)
	simpleAgentPattern := patterns["simpleAgent"]

	lines := strings.Split(content, "\n")
	var out []string
	changed := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		match := simpleAgentPattern.FindStringSubmatch(line)
		if match == nil {
			out = append(out, line)
			continue
		}

		original := []string{line}
		indent := match[1]
		varName := match[2]
		method := match[3]
		urlExpr := strings.TrimSpace(match[4])
		errName := chooseErrName(out, lines, i)

		cfg := simpleAgentConfig{headers: map[string]headerValue{}, params: map[string]string{}}
		callFound := false
		failed := false
		callIndex := i
		var replacement []string

		for j := i + 1; j < len(lines); j++ {
			l := lines[j]
			original = append(original, l)
			if m := headerSetSimplePattern.FindStringSubmatch(l); len(m) > 0 && m[2] == varName {
				key, ok := unquoteLiteral(strings.TrimSpace(m[3]))
				if !ok {
					failed = true
					break
				}
				val, vok := unquoteLiteral(strings.TrimSpace(m[4]))
				if !vok {
					failed = true
					break
				}
				cfg.headers[key] = headerValue{value: val}
				cfg.config = true
				continue
			}
			if m := queryStringPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == varName {
				params, ok := parseQueryParams(strings.TrimSpace(m[3]))
				if !ok {
					failed = true
					break
				}
				for k, v := range params {
					cfg.params[k] = v
				}
				cfg.config = true
				continue
			}
			if m := timeoutPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == varName {
				cfg.timeout = strings.TrimSpace(m[3])
				cfg.config = true
				continue
			}
			if m := basicAuthPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == varName {
				userExpr := strings.TrimSpace(m[3])
				passExpr := strings.TrimSpace(m[4])
				if user, ok := unquoteLiteral(userExpr); ok {
					if pass, okp := unquoteLiteral(passExpr); okp {
						cfg.headers["Authorization"] = headerValue{value: "Basic " + basicAuthLiteral(user, pass)}
						cfg.config = true
						continue
					}
				}

				cfg.headers["Authorization"] = headerValue{value: "\"Basic \" + base64.StdEncoding.EncodeToString([]byte(" + userExpr + " + \":\" + " + passExpr + "))", raw: true}
				cfg.config = true
				continue
			}
			if m := tlsConfigPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == varName {
				cfg.tlsConfig = strings.TrimSpace(m[3])
				cfg.config = true
				continue
			}
			if m := jsonBodyPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == varName {
				cfg.body = strings.TrimSpace(m[3])
				cfg.config = true
				continue
			}
			if m := bodyPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == varName {
				cfg.body = strings.TrimSpace(m[4])
				cfg.config = true
				continue
			}
			// Handle Debug() - not supported in v3, just skip (v3 uses hooks instead)
			if m := debugPattern.FindStringSubmatch(l); len(m) > 0 && m[2] == varName {
				continue
			}
			// Handle Reuse() - not needed in v3, just skip
			if m := reusePattern.FindStringSubmatch(l); len(m) > 0 && m[2] == varName {
				continue
			}
			if m := agentBytesCallPattern.FindStringSubmatch(l); len(m) > 0 && m[5] == varName {
				replacement = buildSimpleAgentReplacement(indent, urlExpr, method, cfg, m[2], m[3], m[4], varName, "bytes", "", m[1], ":=", errName)
				callFound = true
				callIndex = j
				break
			}
			if m := agentStringCallPattern.FindStringSubmatch(l); len(m) > 0 && m[5] == varName {
				replacement = buildSimpleAgentReplacement(indent, urlExpr, method, cfg, m[2], m[3], m[4], varName, "string", "", m[1], ":=", errName)
				callFound = true
				callIndex = j
				break
			}
			if m := agentStructCallPattern.FindStringSubmatch(l); len(m) > 0 && m[5] == varName {
				replacement = buildSimpleAgentReplacement(indent, urlExpr, method, cfg, m[2], m[3], m[4], varName, "struct", strings.TrimSpace(m[6]), m[1], ":=", errName)
				callFound = true
				callIndex = j
				break
			}

			failed = true
			break
		}

		if !callFound || failed {
			out = append(out, original...)
			i = i + len(original) - 1
			continue
		}

		out = append(out, replacement...)
		changed = true
		i = callIndex
	}

	return strings.Join(out, "\n"), changed
}

func buildSimpleAgentReplacement(indent, urlExpr, method string, cfg simpleAgentConfig, statusVar, bodyVar, assignOp, varName, callType, structTarget, callIndent, errAssign, errName string) []string {
	config := buildSimpleConfig(cfg)
	var lines []string

	respLine := fmt.Sprintf("%s%s, %s %s client.%s(%s%s)", indent, varName, errName, errAssign, method, urlExpr, config)
	lines = append(lines, respLine)

	statusName := strings.TrimSpace(statusVar)
	bodyName := strings.TrimSpace(bodyVar)
	statusInit := fmt.Sprintf("%svar %s int", callIndent, statusName)
	bodyInit := fmt.Sprintf("%svar %s []byte", callIndent, bodyName)
	if callType == callTypeString {
		bodyInit = fmt.Sprintf("%svar %s string", callIndent, bodyName)
	}
	if assignOp == "=" {
		statusInit = fmt.Sprintf("%s%s = 0", callIndent, statusName)
		if callType == callTypeString {
			bodyInit = fmt.Sprintf("%s%s = \"\"", callIndent, bodyName)
		} else {
			bodyInit = fmt.Sprintf("%s%s = nil", callIndent, bodyName)
		}
	}
	lines = append(lines, statusInit, bodyInit)
	lines = append(lines, fmt.Sprintf("%sif %s == nil {", callIndent, errName))
	status := fmt.Sprintf("%s\t%s = %s.StatusCode()", callIndent, strings.TrimSpace(statusVar), varName)
	bodyCall := "Body()"
	if callType == callTypeString {
		bodyCall = "String()"
	}
	body := fmt.Sprintf("%s\t%s = %s.%s", callIndent, strings.TrimSpace(bodyVar), varName, bodyCall)
	lines = append(lines, status, body)
	if callType == "struct" {
		lines = append(lines, fmt.Sprintf("%s\t%s = %s.JSON(%s)", callIndent, errName, varName, structTarget))
	}
	lines = append(lines, callIndent+"}")

	return lines
}

func buildSimpleConfig(cfg simpleAgentConfig) string {
	var fields []string
	if len(cfg.headers) > 0 {
		var keys []string
		for k := range cfg.headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var items []string
		for _, k := range keys {
			hv := cfg.headers[k]
			if hv.raw {
				items = append(items, fmt.Sprintf("%q: %s", k, hv.value))
				continue
			}

			items = append(items, fmt.Sprintf("%q: %q", k, hv.value))
		}
		fields = append(fields, fmt.Sprintf("Header: map[string]string{%s}", strings.Join(items, ", ")))
	}
	if len(cfg.params) > 0 {
		var keys []string
		for k := range cfg.params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var items []string
		for _, k := range keys {
			items = append(items, fmt.Sprintf("%q: %q", k, cfg.params[k]))
		}
		fields = append(fields, fmt.Sprintf("Param: map[string]string{%s}", strings.Join(items, ", ")))
	}
	if cfg.body != "" {
		fields = append(fields, "Body: "+cfg.body)
	}
	if cfg.timeout != "" {
		fields = append(fields, "Timeout: "+cfg.timeout)
	}
	if cfg.tlsConfig != "" {
		fields = append(fields, "TLSConfig: "+cfg.tlsConfig)
	}

	if len(fields) == 0 {
		return ""
	}

	return fmt.Sprintf(", client.Config{%s}", strings.Join(fields, ", "))
}

func parseQueryParams(expr string) (map[string]string, bool) {
	value, ok := unquoteLiteral(expr)
	if !ok {
		return nil, false
	}
	result := make(map[string]string)
	for _, pair := range strings.Split(value, "&") {
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			return nil, false
		}
		result[kv[0]] = kv[1]
	}
	return result, true
}

func unquoteLiteral(expr string) (string, bool) {
	val := strings.TrimSpace(expr)
	if strings.HasPrefix(val, "\"") || strings.HasPrefix(val, "`") {
		unquoted, err := strconv.Unquote(val)
		if err != nil {
			return "", false
		}
		return unquoted, true
	}
	return val, false
}

func basicAuthLiteral(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
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

	return fmt.Sprintf("%s%s, err := client.%s(%s, client.Config{Body: %s})\n%svar %s int\n%svar %s []byte\n%sif err == nil {\n%s\t%s = %s.StatusCode()\n%s\t%s = %s.Body()\n%s}", indent, varInit, method, urlArg, bodyArg, callIndent, statusVar, callIndent, bodyVar, callIndent, callIndent, statusVar, varInit, callIndent, bodyVar, varInit, callIndent), true
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

	return fmt.Sprintf("%s%s, err := client.%s(%s)\n%svar %s int\n%svar %s []byte\n%sif err == nil {\n%s\t%s = %s.StatusCode()\n%s\t%s = %s.Body()\n%s}", indent, varInit, method, urlArg, callIndent, statusVar, callIndent, bodyVar, callIndent, callIndent, statusVar, varInit, callIndent, bodyVar, varInit, callIndent), true
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

	return fmt.Sprintf("%s%s, err := client.%s(%s, client.Config{Body: %s})\n%svar %s int\n%svar %s string\n%sif err == nil {\n%s\t%s = %s.StatusCode()\n%s\t%s = %s.String()\n%s}", indent, varInit, method, urlArg, bodyArg, callIndent, statusVar, callIndent, bodyVar, callIndent, callIndent, statusVar, varInit, callIndent, bodyVar, varInit, callIndent), true
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

	return fmt.Sprintf("%s%s, err := client.%s(%s)\n%svar %s int\n%svar %s string\n%sif err == nil {\n%s\t%s = %s.StatusCode()\n%s\t%s = %s.String()\n%s}", indent, varInit, method, urlArg, callIndent, statusVar, callIndent, bodyVar, callIndent, callIndent, statusVar, varInit, callIndent, bodyVar, varInit, callIndent), true
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

	return fmt.Sprintf("%s%s, err := client.%s(%s, client.Config{Body: %s})\n%svar %s int\n%svar %s []byte\n%sif err == nil {\n%s\t%s = %s.StatusCode()\n%s\t%s = %s.Body()\n%s\terr = %s.JSON(%s)\n%s}", indent, varInit, method, urlArg, bodyArg, callIndent, statusVar, callIndent, bodyVar, callIndent, callIndent, statusVar, varInit, callIndent, bodyVar, varInit, callIndent, varInit, target, callIndent), true
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

	return fmt.Sprintf("%s%s, err := client.%s(%s)\n%svar %s int\n%svar %s []byte\n%sif err == nil {\n%s\t%s = %s.StatusCode()\n%s\t%s = %s.Body()\n%s\terr = %s.JSON(%s)\n%s}", indent, varInit, method, urlArg, callIndent, statusVar, callIndent, bodyVar, callIndent, callIndent, statusVar, varInit, callIndent, bodyVar, varInit, callIndent, varInit, target, callIndent), true
}

func ensureImport(content, pkg string) string {
	importLiteral := fmt.Sprintf("%q", pkg)
	if strings.Contains(content, importLiteral) {
		return content
	}

	blockRegex := regexp.MustCompile(`(?m)^import \(([^)]*)\)`) // import block
	if blockRegex.MatchString(content) {
		return blockRegex.ReplaceAllStringFunc(content, func(m string) string {
			return strings.Replace(m, ")", "\t"+importLiteral+"\n)", 1)
		})
	}

	singleImport := regexp.MustCompile(`(?m)^import\s+\"([^\"]+)\"`)
	if matches := singleImport.FindStringSubmatch(content); len(matches) == 2 {
		existing := matches[1]
		if existing == pkg {
			return content
		}
		return singleImport.ReplaceAllString(content, fmt.Sprintf("import (\n\t%q\n\t%q\n)", existing, pkg))
	}

	packageRegex := regexp.MustCompile(`(?m)^package\s+\w+`)
	if match := packageRegex.FindString(content); match != "" {
		return strings.Replace(content, match, match+"\n\nimport \""+pkg+"\"", 1)
	}

	return "import \"" + pkg + "\"\n" + content
}

func ensureClientImport(content string) string {
	updated := ensureImport(content, "github.com/gofiber/fiber/v3/client")
	if strings.Contains(updated, "base64.StdEncoding") {
		updated = ensureImport(updated, "encoding/base64")
	}

	return updated
}

func rewriteClientErrorHandling(content string) string {
	// Only rewrite when we see the legacy multi-error usage patterns. This avoids
	// mutating unrelated identifiers such as custom "errs" slices.
	baseLegacy := clientErrIfPattern.MatchString(content) || clientErrLenPattern.MatchString(content) || clientErrComparePattern.MatchString(content) || clientErrMapPattern.MatchString(content)
	declaredErrs := clientErrsDeclPattern.MatchString(content)
	if !baseLegacy {
		// Declaration-only cases keep errs slices intact.
		return content
	}

	hasLegacyErrs := baseLegacy || declaredErrs
	if !hasLegacyErrs {
		return content
	}

	updated := clientErrIfPattern.ReplaceAllString(content, "if err != nil {")
	updated = clientErrLenPattern.ReplaceAllString(updated, "err != nil")
	updated = clientErrComparePattern.ReplaceAllString(updated, "err != nil")
	updated = clientErrMapPattern.ReplaceAllString(updated, `"err": err`)

	errToken := regexp.MustCompile(`\berrs\b`)
	lines := strings.Split(updated, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "var errs") || strings.HasPrefix(trimmed, "errs") {
			continue
		}

		lines[i] = errToken.ReplaceAllString(line, "err")
	}

	updated = strings.Join(lines, "\n")

	return updated
}

// removeUnusedFiberImport removes unused fiber/v2 or fiber/v3 imports
// when they are no longer needed after migration to the client package.
// Handles: single-line imports, import blocks, and aliased imports.
func removeUnusedFiberImport(content string) string {
	// Extract all import aliases for fiber
	aliases := findFiberImportAliases(content)
	if len(aliases) == 0 {
		// No fiber import found
		return content
	}

	// Check if any alias is still used in the code (excluding imports)
	importContent := extractImportSection(content)
	contentWithoutImports := strings.Replace(content, importContent, "", 1)

	for _, alias := range aliases {
		// Check for usage like "alias." in code
		usagePattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(alias) + `\.`)
		if usagePattern.MatchString(contentWithoutImports) {
			return content
		}
	}

	// No alias is used, remove the fiber import
	updated := removeFiberImportLine(content)
	updated = cleanupEmptyImportBlock(updated)
	return updated
}

// findFiberImportAliases finds all import aliases for fiber/v2 or fiber/v3
// Returns slice of aliases (e.g., ["fiber"] for `import "..."` or ["f"] for `import f "..."`)
func findFiberImportAliases(content string) []string {
	var aliases []string

	// Pattern for aliased import in block: `alias "github.com/gofiber/fiber/v3"`
	aliasedBlockPattern := regexp.MustCompile(`(?m)^\s*(\w+)\s+"github\.com/gofiber/fiber/v[23]"\s*$`)
	if matches := aliasedBlockPattern.FindAllStringSubmatch(content, -1); matches != nil {
		for _, m := range matches {
			aliases = append(aliases, m[1])
		}
	}

	// Pattern for non-aliased import in block: `"github.com/gofiber/fiber/v3"`
	nonAliasedBlockPattern := regexp.MustCompile(`(?m)^\s*"github\.com/gofiber/fiber/v[23]"\s*$`)
	if nonAliasedBlockPattern.MatchString(content) {
		aliases = append(aliases, "fiber")
	}

	// Pattern for single-line aliased import: `import alias "github.com/gofiber/fiber/v3"`
	singleAliasedPattern := regexp.MustCompile(`(?m)^import\s+(\w+)\s+"github\.com/gofiber/fiber/v[23]"\s*$`)
	if matches := singleAliasedPattern.FindAllStringSubmatch(content, -1); matches != nil {
		for _, m := range matches {
			aliases = append(aliases, m[1])
		}
	}

	// Pattern for single-line non-aliased import: `import "github.com/gofiber/fiber/v3"`
	singleNonAliasedPattern := regexp.MustCompile(`(?m)^import\s+"github\.com/gofiber/fiber/v[23]"\s*$`)
	if singleNonAliasedPattern.MatchString(content) {
		aliases = append(aliases, "fiber")
	}

	return aliases
}

// removeFiberImportLine removes the fiber import line from content
// Handles both single-line imports and imports within a block, with or without aliases
func removeFiberImportLine(content string) string {
	// Remove single-line import (with or without alias)
	singleLinePattern := regexp.MustCompile(`(?m)^import\s+(\w+\s+)?"github\.com/gofiber/fiber/v[23]"\s*\n?`)
	content = singleLinePattern.ReplaceAllString(content, "")

	// Remove import line from block (with or without alias)
	blockLinePattern := regexp.MustCompile(`(?m)^\s*(\w+\s+)?"github\.com/gofiber/fiber/v[23]"\s*\n?`)
	content = blockLinePattern.ReplaceAllString(content, "")

	return content
}

// cleanupEmptyImportBlock removes empty import blocks like `import (\n)`
func cleanupEmptyImportBlock(content string) string {
	// Remove import blocks that only contain whitespace/newlines
	emptyBlockPattern := regexp.MustCompile(`(?m)^import\s*\(\s*\)\s*\n?`)
	content = emptyBlockPattern.ReplaceAllString(content, "")

	// Remove import blocks with only whitespace between parentheses
	emptyBlockWithWhitespace := regexp.MustCompile(`(?ms)^import\s*\(\s*\)\s*\n?`)
	content = emptyBlockWithWhitespace.ReplaceAllString(content, "")

	return content
}

func extractImportSection(content string) string {
	blockRegex := regexp.MustCompile(`(?ms)^import \([^)]*\)`)
	if match := blockRegex.FindString(content); match != "" {
		return match
	}

	// Single-line import with optional alias
	singleImport := regexp.MustCompile(`(?m)^import\s+(\w+\s+)?"[^"]+"\s*$`)
	if match := singleImport.FindString(content); match != "" {
		return match
	}

	return ""
}
