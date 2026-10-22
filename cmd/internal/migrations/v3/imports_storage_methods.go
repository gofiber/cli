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
	// foreign says that it embeds a type of a package outside the standard
	// library, whose methods the scan does not know.
	foreign bool
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

	changed, err := internal.ChangeFileContentInfo(cwd, pkgs.addMethods)
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
			case "vendor", "node_modules", ".git":
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
		if id, ok := x.X.(*ast.Ident); !ok || !importsFromStdlib(file, id.Name) {
			t.foreign = true
		}
	default:
		t.foreign = true
	}
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

// bringsMethods reports whether the types t embeds can bring one of the methods
// along: a type of another package might, and so does a type of this package
// that declares one of them.
func (t *importsStorageType) bringsMethods(pkg map[string]*importsStorageType, methods []string, depth int) bool {
	if t.foreign {
		return true
	}
	if depth > importsMaxDepth {
		return false
	}
	for _, name := range t.embedded {
		inner := pkg[name]
		if inner == nil {
			// Declared where the scan could not read it.
			return true
		}
		if slices.ContainsFunc(methods, inner.declares) || inner.bringsMethods(pkg, methods, depth+1) {
			return true
		}
	}
	return false
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
		if _, dur, ok := strings.Cut(p, "."); ok && dur == "Duration" {
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

// addMethods adds the context methods to the storage types whose Set method is
// declared in the file. The file with Set is the one that names time.Duration, so
// the new methods can spell the type the way the file does.
func (p importsStoragePackages) addMethods(content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "Set(") {
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

	var edits editSet
	added := false
	for _, decl := range g.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Set" || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		name := importsReceiverType(fn.Recv.List[0].Type)
		typ := pkg[name]
		if typ == nil || !typ.isStorage() {
			continue
		}
		if set := typ.methods["Set"]; set.path != fi.Path || set.offset != g.offset(fn.Pos()) {
			continue
		}
		missing := typ.missing()
		if len(missing) == 0 {
			continue
		}
		if typ.bringsMethods(pkg, missing, 0) {
			internal.Notef(fi, g.line(fn.Pos()),
				"%s has the methods of Fiber v2's Storage but embeds another type, so the CLI did not add the context methods of fiber.Storage (%s); add the ones the embedded type does not bring.",
				name, strings.Join(missing, ", "))
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
		edits.replace(at, at, importsContextMethods(g, fn, ctx, missing))
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

// importsEndOfLine returns the offset just after the line break that ends the line
// holding offset, or the end of the source.
func importsEndOfLine(src string, offset int) int {
	if nl := strings.IndexByte(src[offset:], '\n'); nl >= 0 {
		return offset + nl + 1
	}
	return len(src)
}

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
func importsContextMethods(g *goSource, set *ast.FuncDecl, ctx string, missing []string) string {
	recv := set.Recv.List[0]
	name := "s"
	if len(recv.Names) == 1 && recv.Names[0].Name != "_" && !slices.Contains([]string{"key", "val", "exp", ctx}, recv.Names[0].Name) {
		name = recv.Names[0].Name
	}
	receiver := "(" + name + " " + g.text(recv.Type) + ")"

	// Spell the types the way Set does.
	var params []ast.Expr
	for _, f := range set.Type.Params.List {
		for range max(len(f.Names), 1) {
			params = append(params, f.Type)
		}
	}
	valueType, expType := g.text(params[1]), g.text(params[2])

	parts := []string{`
// The methods below make the type a fiber.Storage of Fiber v3. They call the ones above, so a
// canceled context is not honored: TODO(fiber migrate): pass ctx on to the backing store.
`}
	for _, method := range missing {
		parts = append(parts, fmt.Sprintf(importsContextTemplates[method], receiver, ctx, name, valueType, expType))
	}
	return strings.Join(parts, "")
}
