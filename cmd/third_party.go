package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	latestContribVersionFn  = latestContribVersion
	latestStorageVersionFn  = func(module, major string) string { return latestThirdPartyVersion("storage", module, major) }
	latestTemplateVersionFn = func(module, major string) string { return latestThirdPartyVersion("template", module, major) }
)

const vendorDir = "vendor"

const (
	contribModulePrefix = "github.com/gofiber/contrib/v3/"
	contribRepoPrefix   = "gofiber/contrib/v3"
)

func refreshContrib(cmd *cobra.Command, cwd, hash string) (bool, error) {
	modules, err := findContribModules(cwd)
	if err != nil {
		return false, fmt.Errorf("find modules: %w", err)
	}
	if len(modules) == 0 {
		return false, nil
	}

	versions := make(map[string]string, len(modules))
	seenModules := make(map[string]struct{}, len(modules))
	if hash == "" {
		reader := bufio.NewReader(cmd.InOrStdin())
		for _, m := range modules {
			targetModule := internal.NormalizeContribModule(m)
			if _, ok := seenModules[targetModule]; ok {
				continue
			}
			seenModules[targetModule] = struct{}{}
			latest := latestContribVersionFn(targetModule)
			prompt := fmt.Sprintf("Version for %s%s (default %s): ", contribModulePrefix, targetModule, latest)
			cmd.Print(prompt)
			line, err := reader.ReadString('\n')
			if err != nil && err != io.EOF {
				return false, fmt.Errorf("read input: %w", err)
			}
			v := strings.TrimSpace(line)
			if v == "" {
				v = latest
			}
			if v != "" {
				versions[targetModule] = v
			}
		}
	} else {
		for _, m := range modules {
			targetModule := internal.NormalizeContribModule(m)
			if _, ok := seenModules[targetModule]; ok {
				continue
			}
			seenModules[targetModule] = struct{}{}
			latest := latestContribVersionFn(targetModule)
			if latest == "" {
				continue
			}
			base, err := semver.NewVersion(strings.TrimPrefix(latest, "v"))
			if err != nil {
				return false, fmt.Errorf("parse version: %w", err)
			}
			pv, err := pseudoVersionFromHash(contribRepoPrefix, base, hash)
			if err != nil {
				return false, fmt.Errorf("pseudo version: %w", err)
			}
			versions[targetModule] = pv
		}
	}
	if len(versions) == 0 {
		return false, nil
	}

	re := regexp.MustCompile(`"github\.com/gofiber/contrib(?:/v\d+)?/([a-zA-Z0-9_]+)(?:/v\d+)?([^\"]*)"`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllStringFunc(content, func(s string) string {
			sub := re.FindStringSubmatch(s)
			mod := sub[1]
			targetModule := internal.NormalizeContribModule(mod)
			rest := sub[2]
			ver, ok := versions[targetModule]
			if !ok {
				return s
			}
			major := majorFromVersion(ver)
			return fmt.Sprintf("\"%s%s%s\"", contribModulePrefix+targetModule, majorPath(major), rest)
		})
	})
	if err != nil {
		return false, fmt.Errorf("refresh imports: %w", err)
	}

	modFile := filepath.Join(cwd, "go.mod")
	b, err := os.ReadFile(modFile) // #nosec G304
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read go.mod: %w", err)
	}
	changedMod := false
	if err == nil {
		content := string(b)
		for mod, ver := range versions {
			major := majorFromVersion(ver)
			newLine := fmt.Sprintf(`${1}%s%s %s`, contribModulePrefix+mod, majorPath(major), ver)
			for _, oldModule := range internal.ContribModuleAliases(mod) {
				re := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*(?:require\s+)?)github.com/gofiber/contrib/(?:v\d+/)?%s(?:/v\d+)?\s+v[\w\.-]+`, regexp.QuoteMeta(oldModule)))
				replaced := re.ReplaceAllString(content, newLine)
				if replaced != content {
					content = replaced
					changedMod = true
				}
			}
		}
		if changedMod {
			if err := os.WriteFile(modFile, []byte(content), 0o600); err != nil {
				return false, fmt.Errorf("write go.mod: %w", err)
			}
		}
	}

	if changed || changedMod {
		cmd.Println("Refreshing contrib packages")
	}
	return changed || changedMod, nil
}

func findContribModules(cwd string) ([]string, error) {
	modules := make(map[string]struct{})
	re := regexp.MustCompile(`(?:^|[^\w])github\.com/gofiber/contrib/(?:v\d+/)?([a-zA-Z0-9_]+)\b`) // capture module name, anchored to avoid partial matches
	err := filepath.WalkDir(cwd, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk path: %w", err)
		}
		if d.IsDir() {
			if d.Name() == vendorDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		b, err := os.ReadFile(path) // #nosec G304
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		matches := re.FindAllStringSubmatch(string(b), -1)
		for _, m := range matches {
			modules[m[1]] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", cwd, err)
	}
	res := make([]string, 0, len(modules))
	for m := range modules {
		res = append(res, m)
	}
	sort.Strings(res)
	return res, nil
}

func latestContribVersion(module string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := fmt.Sprintf("https://proxy.golang.org/%s%s/@latest", contribModulePrefix, module)
	b, status, err := cachedGET(ctx, url, nil)
	if err != nil || status != 200 {
		return ""
	}
	var data struct {
		Version string `json:"Version"` //nolint:tagliatelle // field name defined by proxy
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return ""
	}
	return data.Version
}

func majorFromVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	idx := strings.IndexAny(v, ".-")
	if idx >= 0 {
		v = v[:idx]
	}
	return "v" + v
}

func majorPath(major string) string {
	if major == "" || major == "v0" || major == "v1" {
		return ""
	}
	return "/" + major
}

func refreshThirdParty(cmd *cobra.Command, cwd, hash, repo, label string, latestFn func(string, string) string) (bool, error) {
	modules, err := findThirdPartyModules(cwd, repo)
	if err != nil {
		return false, fmt.Errorf("find modules: %w", err)
	}
	if len(modules) == 0 {
		return false, nil
	}

	versions := make(map[string]string, len(modules))
	for mod, curMajor := range modules {
		latest := latestFn(mod, curMajor)
		if latest == "" {
			continue
		}
		ver := latest
		if hash != "" {
			base, err := semver.NewVersion(strings.TrimPrefix(latest, "v"))
			if err != nil {
				return false, fmt.Errorf("parse version: %w", err)
			}
			pv, err := pseudoVersionFromHash("gofiber/"+repo, base, hash)
			if err != nil {
				return false, fmt.Errorf("pseudo version: %w", err)
			}
			ver = pv
		}
		versions[mod] = ver
	}
	if len(versions) == 0 {
		return false, nil
	}

	re := regexp.MustCompile(fmt.Sprintf(`"github\.com/gofiber/%s/([a-zA-Z0-9_]+)(?:/v\d+)?([^\"]*)"`, regexp.QuoteMeta(repo)))
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllStringFunc(content, func(s string) string {
			sub := re.FindStringSubmatch(s)
			mod := sub[1]
			rest := sub[2]
			ver, ok := versions[mod]
			if !ok {
				return s
			}
			major := majorFromVersion(ver)
			return fmt.Sprintf("\"github.com/gofiber/%s/%s%s%s\"", repo, mod, majorPath(major), rest)
		})
	})
	if err != nil {
		return false, fmt.Errorf("refresh imports: %w", err)
	}

	modFile := filepath.Join(cwd, "go.mod")
	b, err := os.ReadFile(modFile) // #nosec G304
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read go.mod: %w", err)
	}
	changedMod := false
	if err == nil {
		content := string(b)
		for mod, ver := range versions {
			major := majorFromVersion(ver)
			re := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*(?:require\s+)?)github.com/gofiber/%s/%s(?:/v\d+)?\s+v[\w\.-]+`, regexp.QuoteMeta(repo), regexp.QuoteMeta(mod)))
			newLine := fmt.Sprintf(`${1}github.com/gofiber/%s/%s%s %s`, repo, mod, majorPath(major), ver)
			replaced := re.ReplaceAllString(content, newLine)
			if replaced != content {
				content = replaced
				changedMod = true
			}
		}
		if changedMod {
			if err := os.WriteFile(modFile, []byte(content), 0o600); err != nil {
				return false, fmt.Errorf("write go.mod: %w", err)
			}
		}
	}

	if changed || changedMod {
		cmd.Printf("Refreshing %s packages\n", label)
	}
	return changed || changedMod, nil
}

func refreshStorage(cmd *cobra.Command, cwd, hash string) (bool, error) {
	return refreshThirdParty(cmd, cwd, hash, "storage", "storage", latestStorageVersionFn)
}

func refreshTemplates(cmd *cobra.Command, cwd, hash string) (bool, error) {
	return refreshThirdParty(cmd, cwd, hash, "template", "template", latestTemplateVersionFn)
}

func findThirdPartyModules(cwd, repo string) (map[string]string, error) {
	modules := make(map[string]string)
	re := regexp.MustCompile(fmt.Sprintf(`(?:^|[^\w])github\.com/gofiber/%s/([a-zA-Z0-9_]+)(?:/(v\d+))?\b`, regexp.QuoteMeta(repo)))
	err := filepath.WalkDir(cwd, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk path: %w", err)
		}
		if d.IsDir() {
			if d.Name() == vendorDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		b, err := os.ReadFile(path) // #nosec G304
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		matches := re.FindAllStringSubmatch(string(b), -1)
		for _, m := range matches {
			modules[m[1]] = m[2]
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", cwd, err)
	}
	return modules, nil
}

func latestThirdPartyVersion(repo, module, major string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := fmt.Sprintf("https://proxy.golang.org/github.com/gofiber/%s/%s", repo, module)
	if major != "" {
		url += "/" + major
	}
	url += "/@latest"
	b, status, err := cachedGET(ctx, url, nil)
	if err != nil || status != 200 {
		return ""
	}
	var data struct {
		Version string `json:"Version"` //nolint:tagliatelle // field name defined by proxy
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return ""
	}
	return data.Version
}
