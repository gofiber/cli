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
	clientErrsDeclPattern       = regexp.MustCompile(`\berrs\s+\[]error\b`)

	acquireAgentPattern = regexp.MustCompile(`(?m)^([ \t]*)(\w+)\s*:=\s*fiber\.AcquireAgent\(\)\s*$`)
	requestFromAgent    = regexp.MustCompile(`^([ \t]*)(\w+)\s*:=\s*(\w+)\.Request\(\)\s*$`)
	headerMethodPattern = regexp.MustCompile(`^([ \t]*)(\w+)\.Header\.SetMethod\(([^)]*)\)\s*$`)
	headerSetPattern    = regexp.MustCompile(`^([ \t]*)(\w+)\.Header\.Set\(([^,]+),\s*([^)]*)\)\s*$`)
	requestURIPattern   = regexp.MustCompile(`^([ \t]*)(\w+)\.SetRequestURI\((.*)\)\s*$`)
	parseCallPattern    = regexp.MustCompile(`^([ \t]*)if\s+err\s*:=\s*(\w+)\.Parse\(\);\s*err\s*!=\s*nil\s*{\s*$`)
	structAssignPattern = regexp.MustCompile(`^([ \t]*)if\s+([^,]+?)\s*,\s*([^,]+?)\s*,\s*errs\s*([:=]?)=\s*(\w+)\.Struct\((.*)\);\s*len\(errs\)\s*>\s*0\s*{\s*$`)
	bytesAssignPattern  = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.Bytes\(\)\s*$`)
	stringAssignPattern = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.String\(\)\s*$`)
)

var (
	simpleAgentPattern     = regexp.MustCompile(`^([ \t]*)(\w+)\s*:=\s*fiber\.(Get|Head|Post|Put|Patch|Delete)\((.*)\)\s*$`)
	headerSetSimplePattern = regexp.MustCompile(`^([ \t]*)(\w+)\.Set\(([^,]+),\s*([^)]*)\)\s*$`)
	queryStringPattern     = regexp.MustCompile(`^([ \t]*)(\w+)\.QueryString\(([^)]*)\)\s*$`)
	timeoutPattern         = regexp.MustCompile(`^([ \t]*)(\w+)\.Timeout\(([^)]*)\)\s*$`)
	jsonBodyPattern        = regexp.MustCompile(`^([ \t]*)(\w+)\.JSON\(([^)]*)\)\s*$`)
	bodyPattern            = regexp.MustCompile(`^([ \t]*)(\w+)\.(Body|BodyString)\(([^)]*)\)\s*$`)
	basicAuthPattern       = regexp.MustCompile(`^([ \t]*)(\w+)\.BasicAuth\(([^,]+),\s*([^)]*)\)\s*$`)
	tlsConfigPattern       = regexp.MustCompile(`^([ \t]*)(\w+)\.TLSConfig\(([^)]*)\)\s*$`)
	agentBytesCallPattern  = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.Bytes\(\)\s*$`)
	agentStringCallPattern = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.String\(\)\s*$`)
	agentStructCallPattern = regexp.MustCompile(`^([ \t]*)([^,]+),\s*([^,]+),\s*errs\s*(=|:=)\s*(\w+)\.Struct\((.+)\)\s*$`)
)

const callTypeString = "string"

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

		reqLine := -1
		var reqMatch []string
		for j := i + 1; j < len(lines); j++ {
			trimmed := strings.TrimSpace(lines[j])
			if trimmed == "" || strings.Contains(lines[j], "ReleaseAgent("+agentVar+")") || strings.HasPrefix(trimmed, "//") {
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

		j := reqLine + 1
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
		methodName := methodFromExpr(methodExpr)
		configLine := buildConfig(headers)
		if structStart >= len(lines) {
			respLine := fmt.Sprintf("%s_, err := client.%s(%s%s)", indent, methodName, uriExpr, configLine)
			out = append(out, respLine)
			out = append(out, parseIndent+"if err != nil {")
			out = append(out, parseBody[:len(parseBody)-1]...)
			out = append(out, parseIndent+"}")

			i = parseEnd
			changed = true
			continue
		}

		structMatch := structAssignPattern.FindStringSubmatch(lines[structStart])
		bytesMatch := bytesAssignPattern.FindStringSubmatch(lines[structStart])
		stringMatch := stringAssignPattern.FindStringSubmatch(lines[structStart])
		if len(structMatch) == 0 && len(bytesMatch) == 0 && len(stringMatch) == 0 {
			respLine := fmt.Sprintf("%s_, err := client.%s(%s%s)", indent, methodName, uriExpr, configLine)
			out = append(out, respLine)
			out = append(out, parseIndent+"if err != nil {")
			out = append(out, parseBody[:len(parseBody)-1]...)
			out = append(out, parseIndent+"}")

			i = structStart - 1
			changed = true
			continue
		}

		switch {
		case len(structMatch) > 0 && structMatch[5] == agentVar:
			statusVar := strings.TrimSpace(structMatch[2])
			bodyVar := strings.TrimSpace(structMatch[3])
			assignOp := structMatch[4]
			if assignOp == "" {
				assignOp = "="
			}
			structTarget := strings.TrimSpace(structMatch[6])

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
		case len(bytesMatch) > 0 && bytesMatch[5] == agentVar:
			statusVar := strings.TrimSpace(bytesMatch[2])
			bodyVar := strings.TrimSpace(bytesMatch[3])
			assignOp := bytesMatch[4]
			if assignOp == "" {
				assignOp = "="
			}

			respLine := fmt.Sprintf("%sresp, err := client.%s(%s%s)", indent, methodName, uriExpr, configLine)
			out = append(out, respLine)
			out = append(out, parseIndent+"if err != nil {")
			out = append(out, parseBody[:len(parseBody)-1]...)
			out = append(out, parseIndent+"}")
			out = append(out, fmt.Sprintf("%s%s %s resp.StatusCode()", indent, statusVar, assignOp))
			out = append(out, fmt.Sprintf("%s%s %s resp.Body()", indent, bodyVar, assignOp))

			i = structStart
			changed = true
			continue
		case len(stringMatch) > 0 && stringMatch[5] == agentVar:
			statusVar := strings.TrimSpace(stringMatch[2])
			bodyVar := strings.TrimSpace(stringMatch[3])
			assignOp := stringMatch[4]
			if assignOp == "" {
				assignOp = "="
			}

			respLine := fmt.Sprintf("%sresp, err := client.%s(%s%s)", indent, methodName, uriExpr, configLine)
			out = append(out, respLine)
			out = append(out, parseIndent+"if err != nil {")
			out = append(out, parseBody[:len(parseBody)-1]...)
			out = append(out, parseIndent+"}")
			out = append(out, fmt.Sprintf("%s%s %s resp.StatusCode()", indent, statusVar, assignOp))
			out = append(out, fmt.Sprintf("%s%s %s resp.String()", indent, bodyVar, assignOp))

			i = structStart
			changed = true
			continue
		default:
			out = append(out, line)
			continue
		}
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
	updated, changedSimple := rewriteSimpleAgentBlocks(content)
	changed := changedSimple

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

func rewriteSimpleAgentBlocks(content string) (string, bool) {
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
			if m := agentBytesCallPattern.FindStringSubmatch(l); len(m) > 0 && m[5] == varName {
				replacement = buildSimpleAgentReplacement(indent, urlExpr, method, cfg, m[2], m[3], m[4], varName, "bytes", "", m[1])
				callFound = true
				callIndex = j
				break
			}
			if m := agentStringCallPattern.FindStringSubmatch(l); len(m) > 0 && m[5] == varName {
				replacement = buildSimpleAgentReplacement(indent, urlExpr, method, cfg, m[2], m[3], m[4], varName, "string", "", m[1])
				callFound = true
				callIndex = j
				break
			}
			if m := agentStructCallPattern.FindStringSubmatch(l); len(m) > 0 && m[5] == varName {
				replacement = buildSimpleAgentReplacement(indent, urlExpr, method, cfg, m[2], m[3], m[4], varName, "struct", strings.TrimSpace(m[6]), m[1])
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

func buildSimpleAgentReplacement(indent, urlExpr, method string, cfg simpleAgentConfig, statusVar, bodyVar, assignOp, varName, callType, structTarget, callIndent string) []string {
	config := buildSimpleConfig(cfg)
	var lines []string

	respLine := fmt.Sprintf("%s%s, err := client.%s(%s%s)", indent, varName, method, urlExpr, config)
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
	lines = append(lines, callIndent+"if err == nil {")
	status := fmt.Sprintf("%s\t%s = %s.StatusCode()", callIndent, strings.TrimSpace(statusVar), varName)
	bodyCall := "Body()"
	if callType == callTypeString {
		bodyCall = "String()"
	}
	body := fmt.Sprintf("%s\t%s = %s.%s", callIndent, strings.TrimSpace(bodyVar), varName, bodyCall)
	lines = append(lines, status, body)
	if callType == "struct" {
		lines = append(lines, fmt.Sprintf("%s\terr = %s.JSON(%s)", callIndent, varName, structTarget))
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
	updated := clientErrsDeclPattern.ReplaceAllString(content, "err error")
	updated = clientErrIfPattern.ReplaceAllString(updated, "if err != nil {")
	updated = clientErrLenPattern.ReplaceAllString(updated, "err != nil")
	updated = clientErrComparePattern.ReplaceAllString(updated, "err != nil")
	updated = clientErrMapPattern.ReplaceAllString(updated, `"err": err`)
	updated = clientErrVarPattern.ReplaceAllString(updated, "err")
	return updated
}
