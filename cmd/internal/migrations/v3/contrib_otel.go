package v3

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// The OpenTelemetry Go modules are released together: the core module, the SDK, the
// exporters and the rest of the family share a release, and a module of one release
// imports packages of the core module that another release no longer has (the
// exporters under go.opentelemetry.io/otel/exporters/otlp share internal packages of
// the core module, for one). The v3 module of otelfiber requires a much newer core
// module than a project of the v2 era has, so the move raises the core module and
// leaves the exporters of the project behind; go mod tidy then fails with "module ...
// found, but does not contain package go.opentelemetry.io/otel/exporters/otlp/internal".
// The move raises the other modules of the family with it.

const (
	otelCoreModule   = "go.opentelemetry.io/otel"
	otelModulePrefix = otelCoreModule + "/"
	// otelContribModule is the module of the instrumentation libraries of the OpenTelemetry project
	// (otelhttp, otelgrpc, the propagators), which are released with the core module and
	// import packages that a newer core module no longer has.
	otelContribModule = "go.opentelemetry.io/contrib"
	// otelShort is the name of the v3 module of otelfiber in the table of the moved modules.
	otelShort = "otel"
	// otelMaxNamed is how many modules the note names.
	otelMaxNamed = 4
)

// isOtelFamily reports whether the module is one that is released with the core module of OpenTelemetry.
func isOtelFamily(path string) bool {
	return path == otelCoreModule || strings.HasPrefix(path, otelModulePrefix) ||
		path == otelContribModule || strings.HasPrefix(path, otelContribModule+"/")
}

// otelResult is the answer of the lookups for a requirement of the family.
type otelResult struct {
	req *modfile.Require
	// to is the release the requirement moves to, err why none was found.
	to, err string
}

// otelMoves reports whether this run moves the otelfiber requirement of the go.mod file.
func (r *contribRun) otelMoves(mod *contribGoMod) bool {
	if _, resolved := r.versions[otelShort]; !resolved || r.skipped[otelShort] {
		return false
	}
	return slices.ContainsFunc(r.lines(mod), func(l contribModLine) bool { return !l.replace && l.target.short == otelShort })
}

// contribOtelCoreNeeded returns the release of the core module that the v3 module of otelfiber at
// the version requires, "" when it cannot be read.
func contribOtelCoreNeeded(version string) string {
	found, err := askContribModule(contribV3Prefix+otelShort, version)
	if err != nil || found.GoMod == "" {
		return ""
	}
	b, err := os.ReadFile(found.GoMod) // #nosec G304 -- the go.mod of a module in the module cache
	if err != nil {
		return ""
	}
	f, err := modfile.ParseLax(found.GoMod, b, nil)
	if err != nil {
		return ""
	}
	for _, req := range f.Require {
		if req.Mod.Path == otelCoreModule {
			return req.Mod.Version
		}
	}
	return ""
}

// raiseOtelFamily raises the requirements of the OpenTelemetry modules of a go.mod file whose otelfiber
// requirement moves to the v3 module, to the release that module needs (or the latest one of a module that
// has none of that release), and leaves a note. A module with a replace directive is left alone. It returns
// the new text of the file; content is the text after the move.
func (r *contribRun) raiseOtelFamily(mod *contribGoMod, content string) string {
	if !r.otelMoves(mod) {
		return content
	}
	f, err := modfile.Parse(mod.path, []byte(content), nil)
	if err != nil {
		return content
	}

	var family []*modfile.Require
	for _, req := range f.Require {
		path := req.Mod.Path
		if isOtelFamily(path) &&
			!slices.ContainsFunc(f.Replace, func(rep *modfile.Replace) bool { return rep.Old.Path == path }) {
			family = append(family, req)
		}
	}
	if len(family) == 0 {
		return content
	}

	need := contribOtelCoreNeeded(r.versions[otelShort])
	if need != "" {
		for _, req := range family {
			// The project has the release already: the move adds no skew.
			if req.Mod.Path == otelCoreModule && semver.Compare(req.Mod.Version, need) >= 0 {
				return content
			}
		}
	}

	raises, failed := otelLookups(family, need)
	if len(raises) == 0 && len(failed) == 0 {
		return content
	}

	var edits editSet
	names := make([]string, 0, len(raises))
	for _, raise := range raises {
		start, end := raise.req.Syntax.Start.Byte, raise.req.Syntax.End.Byte
		l := contribModLine{path: raise.req.Mod.Path, version: raise.req.Mod.Version}
		edits.replace(start, end, contribRewriteModLine(content[start:end], l, l.path, raise.to))
		names = append(names, fmt.Sprintf("%s %s to %s", raise.req.Mod.Path, raise.req.Mod.Version, raise.to))
	}
	if len(raises) > 0 {
		if len(names) > otelMaxNamed {
			names = append(names[:otelMaxNamed], fmt.Sprintf("and %d more", len(raises)-otelMaxNamed))
		}
		why := "the OpenTelemetry modules have to be on one release, or go mod tidy fails on packages that one release has and the other does not"
		if need != "" {
			why = fmt.Sprintf("%s requires %s %s, and %s", contribV3Prefix+otelShort, otelCoreModule, need, why)
		}
		r.noteGoModf(mod.path, raises[0].req.Syntax.Start.Line, "raised %s: %s; check the exporters you configure for API changes between the releases", strings.Join(names, ", "), why)
	}
	for _, miss := range failed {
		r.noteGoModf(mod.path, miss.req.Syntax.Start.Line,
			"could not look up a newer release of %s (%s); if go mod tidy fails with \"does not contain package go.opentelemetry.io/otel/...\", raise the OpenTelemetry modules together, for example with go get %s@latest",
			miss.req.Mod.Path, miss.err, miss.req.Mod.Path)
	}
	return edits.apply(content)
}

// otelLookups finds the release each requirement of the family moves to: the release that the v3 module needs,
// or else the latest one. The lookups run side by side. A requirement that is on that release or a newer one is
// left out; one whose lookups failed is returned in failed.
func otelLookups(family []*modfile.Require, need string) (raises, failed []otelResult) {
	results := make([]otelResult, len(family))
	var wg sync.WaitGroup
	for i, req := range family {
		wg.Go(func() {
			results[i] = otelResult{req: req}
			var errs []string
			for _, query := range otelQueries(need) {
				found, err := askContribModule(req.Mod.Path, query)
				if err == nil {
					results[i].to = found.Version
					return
				}
				errs = append(errs, strings.Join(strings.Fields(err.Error()), " "))
			}
			results[i].err = strings.Join(errs, "; ")
		})
	}
	wg.Wait()

	for _, res := range results {
		switch {
		case res.err != "":
			failed = append(failed, res)
		case semver.Compare(res.to, res.req.Mod.Version) > 0:
			raises = append(raises, res)
		}
	}
	return raises, failed
}

// otelQueries are the versions asked for, in order: the release the v3 module needs, which a module of the
// family has when it is released with the core module, and the latest.
func otelQueries(need string) []string {
	if need == "" {
		return []string{"latest"}
	}
	return []string{need, "latest"}
}
