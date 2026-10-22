package v3

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

const importsContextPath = "context"

// importsStorageContextMethods are the methods fiber.Storage gained in v3.
var importsStorageContextMethods = strings.Fields("GetWithContext SetWithContext DeleteWithContext ResetWithContext")

// importsStorageSignatures are the methods that make a type a fiber.Storage of Fiber v2,
// with their signatures. The type of the expiration is matched by its name,
// whatever the file calls the time package.
const importsStorageSignatures = `Get(string) ([]byte, error)
Set(string, []byte, time.Duration) error
Delete(string) error
Reset() error
Close() error`

// importsStorageMethod is a method as the scan of the package saw it: its declaration
// and where in which file it is.
type importsStorageMethod struct {
	decl *ast.FuncDecl
	path string
	// offset is where the declaration starts and end where it ends.
	offset, end int
}

// importsStorageType is what the package says about one named type: the methods it
// declares and the types it embeds, which can bring methods along.
type importsStorageType struct {
	methods map[string]importsStorageMethod
	// promoted are the methods of an interface type.
	promoted map[string]bool
	// embedded are the names of the types of the package that the type embeds.
	embedded []string
	// selectors are the types of other packages, outside the standard library,
	// that the type embeds.
	selectors []importsSelector
	// foreign says that it embeds something the scan can say nothing about.
	foreign bool
}

// importsSelector is an embedded type of another package: the import path of the
// package, "" when the file does not say, and the name of the type.
type importsSelector struct {
	path string
	name string
}

// importsStoragePackages indexes the types of every package below a directory by the
// directory and package name, then by type name. The methods of a type may be
// declared in any file of its package.
type importsStoragePackages map[string]map[string]*importsStorageType

// MigrateStorageInterface gives the hand-written implementations of Fiber v2's
// Storage interface the four context methods that Fiber v3 added to it, so that
// they still satisfy fiber.Storage. The new methods call the ones that exist and
// ignore the context, which is as much as a storage written for v2 can do.
func MigrateStorageInterface(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	pkgs, err := importsScanStorage(cwd)
	if err != nil {
		return err
	}

	var indexes moduleIndexes
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return pkgs.addMethods(content, fi, &indexes)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate storage implementations: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Adding the context methods to fiber.Storage implementations")
	return nil
}

// importsScanStorage parses the Go files below root, without regard for the
// include and exclude filters: the methods of a type may be in a file that is
// not being migrated.
func importsScanStorage(root string) (importsStoragePackages, error) {
	pkgs := importsStoragePackages{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", nameNodeModules, nameDotGit:
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		src, err := os.ReadFile(path) // #nosec G304 -- reading a source file of the migrated project
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			return nil //nolint:nilerr // a file that does not parse is reported by the migrations that rewrite it
		}
		pkgs.add(path, fset, file)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan for storage implementations: %w", err)
	}
	return pkgs, nil
}

func (p importsStoragePackages) add(path string, fset *token.FileSet, file *ast.File) {
	key := filepath.Dir(path) + "\x00" + file.Name.Name
	pkg := p[key]
	if pkg == nil {
		pkg = map[string]*importsStorageType{}
		p[key] = pkg
	}
	typ := func(name string) *importsStorageType {
		if pkg[name] == nil {
			pkg[name] = &importsStorageType{methods: map[string]importsStorageMethod{}, promoted: map[string]bool{}}
		}
		return pkg[name]
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil && len(d.Recv.List) == 1 {
				if name := importsReceiverType(d.Recv.List[0].Type); name != "" {
					typ(name).methods[d.Name.Name] = importsStorageMethod{
						decl: d, path: path, offset: fset.Position(d.Pos()).Offset, end: fset.Position(d.End()).Offset,
					}
				}
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					typ(ts.Name.Name).declare(ts, file)
				}
			}
		}
	}
}

// importsReceiverType returns the name of the type a method is declared on:
// *Store, Store and *Store[T] are all Store.
func importsReceiverType(e ast.Expr) string {
	if id, ok := importsTypeBase(e).(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// declare records what the declaration of a type says about the methods it
// has: the methods of an interface, and the types that a struct or an interface
// embeds or an alias names, which the type gets methods from. A type of the
// standard library cannot have the context methods of fiber.Storage, one of
// another package might.
func (t *importsStorageType) declare(spec *ast.TypeSpec, file *ast.File) {
	switch x := spec.Type.(type) {
	case *ast.StructType:
		t.embed(x.Fields, file)
	case *ast.InterfaceType:
		for _, m := range x.Methods.List {
			for _, name := range m.Names {
				t.promoted[name.Name] = true
			}
		}
		t.embed(x.Methods, file)
	default:
		if spec.Assign.IsValid() {
			t.embedType(x, file)
		}
	}
}

// embed records the embedded fields of a struct or an interface.
func (t *importsStorageType) embed(fields *ast.FieldList, file *ast.File) {
	for _, f := range fields.List {
		if len(f.Names) == 0 {
			t.embedType(f.Type, file)
		}
	}
}

func (t *importsStorageType) embedType(e ast.Expr, file *ast.File) {
	switch x := importsTypeBase(e).(type) {
	case *ast.Ident:
		if types.Universe.Lookup(x.Name) == nil {
			t.embedded = append(t.embedded, x.Name)
		}
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		switch {
		case !ok:
			t.foreign = true
		case !importsFromStdlib(file, id.Name):
			t.selectors = append(t.selectors, importsSelector{path: importsPathOf(file, id.Name), name: x.Sel.Name})
		}
	default:
		t.foreign = true
	}
}

// importsPathOf returns the import path the file binds name to, "" when it binds
// the name to none.
func importsPathOf(file *ast.File, name string) string {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		local := defaultImportName(path)
		if spec.Name != nil {
			local = spec.Name.Name
		}
		if local == name {
			return path
		}
	}
	return ""
}

// importsFromStdlib reports whether the file binds name to a package of the
// standard library, whose import paths have no dot in their first element.
func importsFromStdlib(file *ast.File, name string) bool {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		local := defaultImportName(path)
		if spec.Name != nil {
			local = spec.Name.Name
		}
		if local == name {
			first, _, _ := strings.Cut(path, "/")
			return !strings.Contains(first, ".")
		}
	}
	return false
}

// importsTypeBase strips the pointer and the type arguments from the type of an
// embedded field or a receiver.
func importsTypeBase(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		default:
			return e
		}
	}
}

// importsIsFiberStorage reports whether the selector names the Storage interface of Fiber.
func importsIsFiberStorage(sel importsSelector) bool {
	return sel.name == cacheStorage && reFiberCorePath.MatchString(sel.path)
}

// importsStorageModule is the module of the storage drivers of Fiber: every type of
// its packages is a storage.
const importsStorageModule = "github.com/gofiber/storage/"

// embedsStorage tells what the types t embeds are: known is set when one of them
// is a storage (the interface of Fiber, a driver, a type of the module that has its
// methods or embeds one), weak when one is a type about which the scan can say
// nothing, which may be one.
func (p importsStoragePackages) embedsStorage(ix func() *moduleIndex, pkg map[string]*importsStorageType, t *importsStorageType, depth int) (known, weak bool) {
	if depth > importsMaxDepth {
		return false, false
	}
	weak = t.foreign
	for _, sel := range t.selectors {
		switch {
		case importsIsFiberStorage(sel) || strings.HasPrefix(sel.path, importsStorageModule):
			known = true
		case p.moduleTypeIsStorage(ix, sel, depth):
			known = true
		default:
			weak = true
		}
	}
	for _, name := range t.embedded {
		inner := pkg[name]
		if inner == nil {
			// Declared where the scan could not read it.
			weak = true
			continue
		}
		if inner.isStorage() || inner.listsStorage() {
			known = true
			continue
		}
		innerKnown, innerWeak := p.embedsStorage(ix, pkg, inner, depth+1)
		known, weak = known || innerKnown, weak || innerWeak
	}
	return known, weak
}

// moduleTypeIsStorage reports whether the type that the selector names, in a
// package of the module, is a storage.
func (p importsStoragePackages) moduleTypeIsStorage(ix func() *moduleIndex, sel importsSelector, depth int) bool {
	dir, ok := ix().dirOf(sel.path)
	if sel.path == "" || !ok {
		return false
	}
	for key, pkg := range p {
		if !strings.HasPrefix(key, dir+"\x00") || strings.HasSuffix(key, "_test") {
			continue
		}
		typ := pkg[sel.name]
		if typ == nil {
			continue
		}
		if typ.isStorage() || typ.listsStorage() {
			return true
		}
		known, _ := p.embedsStorage(ix, pkg, typ, depth+1)
		return known
	}
	return false
}

// listsStorage reports whether the type is an interface that has the methods of
// Fiber v2's Storage.
func (t *importsStorageType) listsStorage() bool {
	for line := range strings.Lines(importsStorageSignatures) {
		name, _, _ := strings.Cut(line, "(")
		if !t.promoted[name] {
			return false
		}
	}
	return true
}

// v2Methods returns the methods of the four that have a context twin in v3 which
// the type declares with the signature of Fiber v2, and the twin it lacks.
func (t *importsStorageType) v2Methods() (declared int, missing []string) {
	for line := range strings.Lines(importsStorageSignatures) {
		name, _, _ := strings.Cut(line, "(")
		m, ok := t.methods[name]
		if !ok || name == "Close" || importsMethodSignature(name, m.decl.Type) != strings.TrimSpace(line) {
			continue
		}
		declared++
		if twin := name + "WithContext"; !t.declares(twin) {
			missing = append(missing, name)
		}
	}
	return declared, missing
}

// declares reports whether the type has a method of its own with the name, or an
// interface type lists it.
func (t *importsStorageType) declares(name string) bool {
	_, ok := t.methods[name]
	return ok || t.promoted[name]
}

// isStorage reports whether the type declares the five methods of Fiber v2's
// Storage interface with their signatures.
func (t *importsStorageType) isStorage() bool {
	for line := range strings.Lines(importsStorageSignatures) {
		name, _, _ := strings.Cut(line, "(")
		m, ok := t.methods[name]
		if !ok || importsMethodSignature(name, m.decl.Type) != strings.TrimSpace(line) {
			return false
		}
	}
	return true
}

// missing returns the context methods the type does not declare.
func (t *importsStorageType) missing() []string {
	var missing []string
	for _, name := range importsStorageContextMethods {
		if _, ok := t.methods[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

// importsMethodSignature renders the signature of a method the way importsStorageSignatures
// does: the parameter types, then the result types in parentheses when there
// are several. A type named Duration is the one from the time package.
func importsMethodSignature(name string, ft *ast.FuncType) string {
	params := importsFieldTypes(ft.Params)
	for i, p := range params {
		if _, dur, ok := strings.Cut(p, "."); ok && dur == nameDuration {
			params[i] = "time.Duration"
		}
	}

	sig := name + "(" + strings.Join(params, ", ") + ")"
	switch results := importsFieldTypes(ft.Results); len(results) {
	case 0:
	case 1:
		sig += " " + results[0]
	default:
		sig += " (" + strings.Join(results, ", ") + ")"
	}
	return sig
}

// importsFieldTypes lists the type of every parameter or result, a name per entry.
func importsFieldTypes(fields *ast.FieldList) []string {
	var out []string
	if fields == nil {
		return out
	}
	for _, f := range fields.List {
		for range max(len(f.Names), 1) {
			out = append(out, types.ExprString(f.Type))
		}
	}
	return out
}

// importsStorageDeclRe finds the files that declare a method of the storage
// interface that has a context twin in v3.
var importsStorageDeclRe = regexp.MustCompile(`\)\s+(?:Get|Set|Delete|Reset)\(`)

// addMethods adds the context methods that v3 asks of a storage to the types of the
// file:
//
//   - a type that has the five methods of Fiber v2's Storage gets the context
//     methods it lacks, in the file with its Set method: that is the file that
//     names time.Duration, so the new methods can spell the type the way the file
//     does;
//   - a type that wraps a storage, which embeds one and declares some of Get, Set,
//     Delete and Reset, gets the twin of each it declares, in the file of the
//     method. The middleware of v3 call the twins, which the embedded storage
//     brings: without its own twin the wrapper is bypassed, and its counters, its
//     tenant keys and its logging with it.
func (p importsStoragePackages) addMethods(content string, fi internal.FileInfo, indexes *moduleIndexes) string {
	if !importsStorageDeclRe.MatchString(content) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}
	pkg := p[filepath.Dir(fi.Path)+"\x00"+g.file.Name.Name]
	if pkg == nil {
		return content
	}

	// How the file refers to the context package, and whether it has to import it.
	ctx := importsNameOf(g, importsContextPath, "context")
	importAs, addImport := "", ctx == ""
	if addImport {
		ctx = "context"
		if importsBinds(g, ctx) {
			ctx, importAs = "stdcontext", "stdcontext"
		}
	}

	var ix *moduleIndex
	resolved := false
	index := func() *moduleIndex {
		if !resolved {
			ix, resolved = indexes.of(fi), true
		}
		return ix
	}

	var edits editSet
	added := false
	seen := map[string]bool{}
	for _, decl := range g.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || !slices.Contains(importsOldMethods, fn.Name.Name) {
			continue
		}
		name := importsReceiverType(fn.Recv.List[0].Type)
		typ := pkg[name]
		if typ == nil || seen[name] {
			continue
		}

		var text string
		if typ.isStorage() {
			text = storageMethods(g, fi, fn, typ, ctx)
			seen[name] = text != "" || fn.Name.Name == nameSet
		} else {
			seen[name] = true
			text = p.wrapperMethods(g, fi, pkg, typ, ctx, index)
		}
		if text == "" {
			continue
		}

		// After the last of the type's methods in this file.
		last := g.offset(fn.End())
		for _, m := range typ.methods {
			if m.path == fi.Path {
				last = max(last, m.end)
			}
		}
		at := importsEndOfLine(g.src, last)
		edits.replace(at, at, text)
		added = true
	}
	if !added {
		return content
	}

	out := edits.apply(content)
	if !addImport {
		return out
	}
	g2, ok := parseSource(out)
	if !ok {
		return content
	}
	var imp editSet
	importsAdd(g2, &imp, importAs, importsContextPath)
	return imp.apply(out)
}

// importsOldMethods are the methods of Fiber v2's Storage that v3 gave a twin that
// takes a context.
var importsOldMethods = []string{appMethodGet, nameSet, nameDelete, "Reset"}

// storageMethods renders the context methods a type with the five methods of
// Fiber v2's Storage lacks, when fn is its Set method.
func storageMethods(g *goSource, fi internal.FileInfo, fn *ast.FuncDecl, typ *importsStorageType, ctx string) string {
	if fn.Name.Name != nameSet {
		return ""
	}
	if set := typ.methods["Set"]; set.path != fi.Path || set.offset != g.offset(fn.Pos()) {
		return ""
	}
	missing := typ.missing()
	if len(missing) == 0 {
		return ""
	}
	return importsContextMethods(g, fn, fn, ctx, missing, importsStorageIntro)
}

// wrapperMethods renders the context twins of the methods a storage wrapper
// declares in this file. The type wraps a storage when it embeds one (or a type
// that is one); a type that embeds something the scan can say nothing about is
// taken for one when it declares two of the methods, which is not a pair of
// methods a type without a storage in it has by chance.
func (p importsStoragePackages) wrapperMethods(g *goSource, fi internal.FileInfo, pkg map[string]*importsStorageType, typ *importsStorageType, ctx string, ix func() *moduleIndex) string {
	declared, lacking := typ.v2Methods()
	if declared == 0 || len(lacking) == 0 {
		return ""
	}
	known, weak := p.embedsStorage(ix, pkg, typ, 0)
	if !known && (!weak || declared < 2) {
		return ""
	}

	var missing []string
	var anchor, set *ast.FuncDecl
	for _, old := range lacking {
		m := typ.methods[old]
		if m.path != fi.Path {
			continue
		}
		missing = append(missing, old+"WithContext")
		if anchor == nil {
			anchor = m.decl
		}
		if old == nameSet {
			set = m.decl
		}
	}
	if anchor == nil {
		return ""
	}
	return importsContextMethods(g, anchor, set, ctx, missing, importsWrapperIntro)
}

// importsEndOfLine returns the offset just after the line break that ends the line
// holding offset, or the end of the source.
func importsEndOfLine(src string, offset int) int {
	if nl := strings.IndexByte(src[offset:], '\n'); nl >= 0 {
		return offset + nl + 1
	}
	return len(src)
}

// importsStorageIntro and importsWrapperIntro stand above the context methods
// that are added to a storage and to a wrapper of one.
const (
	importsStorageIntro = `
// The methods below make the type a fiber.Storage of Fiber v3. They call the ones above, so a
// canceled context is not honored: TODO(fiber migrate): pass ctx on to the backing store.
`
	importsWrapperIntro = `
// The middleware of Fiber v3 call the methods below where Fiber v2 called the ones above, and the
// embedded storage brings them: without these the calls skip this type. They call the ones above, so a
// canceled context is not honored: TODO(fiber migrate): pass ctx on to the backing store.
`
)

// importsContextTemplates render the context methods. The arguments are the
// receiver, the identifier of the context package, the receiver's name, and the
// types the file uses for the value and the expiration of Set.
var importsContextTemplates = map[string]string{
	"GetWithContext": `
// GetWithContext calls Get.
func %[1]s GetWithContext(_ %[2]s.Context, key string) ([]byte, error) {
	return %[3]s.Get(key)
}
`,
	"SetWithContext": `
// SetWithContext calls Set.
func %[1]s SetWithContext(_ %[2]s.Context, key string, val %[4]s, exp %[5]s) error {
	return %[3]s.Set(key, val, exp)
}
`,
	"DeleteWithContext": `
// DeleteWithContext calls Delete.
func %[1]s DeleteWithContext(_ %[2]s.Context, key string) error {
	return %[3]s.Delete(key)
}
`,
	"ResetWithContext": `
// ResetWithContext calls Reset.
func %[1]s ResetWithContext(_ %[2]s.Context) error {
	return %[3]s.Reset()
}
`,
}

// importsContextMethods renders the missing context methods of the type whose Set
// method is set. ctx is the identifier of the context package.
func importsContextMethods(g *goSource, anchor, set *ast.FuncDecl, ctx string, missing []string, intro string) string {
	recv := anchor.Recv.List[0]
	name := "s"
	if len(recv.Names) == 1 && recv.Names[0].Name != "_" && !slices.Contains([]string{"key", "val", "exp", ctx}, recv.Names[0].Name) {
		name = recv.Names[0].Name
	}
	receiver := "(" + name + " " + g.text(recv.Type) + ")"

	// Spell the types the way Set does.
	valueType, expType := "", ""
	if set != nil {
		var params []ast.Expr
		for _, f := range set.Type.Params.List {
			for range max(len(f.Names), 1) {
				params = append(params, f.Type)
			}
		}
		valueType, expType = g.text(params[1]), g.text(params[2])
	}

	parts := []string{intro}
	for _, method := range missing {
		parts = append(parts, fmt.Sprintf(importsContextTemplates[method], receiver, ctx, name, valueType, expType))
	}
	return strings.Join(parts, "")
}
