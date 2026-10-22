package v3

import (
	"fmt"
	"regexp"
	"strings"
)

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
