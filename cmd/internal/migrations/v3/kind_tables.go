package v3

import (
	"go/ast"
)

// kindTables hold what the fields and the methods of the structs of a module stand
// for, by their names. A migration that has to tell whether `x.store` or `x.Hub()`
// is a value it looks for, and has no type checker, asks whether the fields called
// store and the methods called Hub say the same thing in every struct of the
// module: if they do, the name is as good as the type.
//
// A kind is a small number each migration chooses; 0 is a kind that cannot be
// told. A name is dropped when two declarations disagree, or when one of them
// cannot be told.
type kindTables struct {
	fields, methods map[string]uint8
}

// kindTable collects the kinds that names stand for.
type kindTable struct {
	kinds map[string]uint8
	mixed map[string]bool
}

func newKindTable() *kindTable {
	return &kindTable{kinds: map[string]uint8{}, mixed: map[string]bool{}}
}

func (t *kindTable) add(name string, kind uint8) {
	if prev, seen := t.kinds[name]; (seen && prev != kind) || kind == 0 {
		t.mixed[name] = true
	}
	t.kinds[name] = kind
}

// result returns the names that every declaration agrees on.
func (t *kindTable) result() map[string]uint8 {
	out := map[string]uint8{}
	for name, kind := range t.kinds {
		if !t.mixed[name] {
			out[name] = kind
		}
	}
	return out
}

// moduleKindTables reads the structs and the methods of the packages of the module,
// the test files left out. classify says what a value of a type written in a file
// is.
func moduleKindTables(ix *moduleIndex, classify func(modType) uint8) kindTables {
	fields, methods := newKindTable(), newKindTable()
	for _, pkg := range ix.packages() {
		for _, d := range pkg.decls(false) {
			switch d.kind {
			case modMethod:
				kind := uint8(0)
				if results := d.results(); len(results) > 0 {
					kind = classify(results[0])
				}
				methods.add(d.name, kind)
			case modTypeDecl:
				if d.typ.Assign.IsValid() {
					continue
				}
				for _, fld := range (modType{d.file, d.typ.Type}).fields() {
					fields.add(fld.name, classify(fld.typ))
				}
			default:
				// functions, variables and constants have neither
			}
		}
	}
	return kindTables{fields: fields.result(), methods: methods.result()}
}

// fileKindTables reads the structs and the methods of one file, for a project that
// is not in a module. classify says what a value of a type is.
func fileKindTables(file *ast.File, classify func(ast.Expr) uint8) kindTables {
	fields, methods := newKindTable(), newKindTable()
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil {
				kind := uint8(0)
				if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
					kind = classify(d.Type.Results.List[0].Type)
				}
				methods.add(d.Name.Name, kind)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, isStruct := ts.Type.(*ast.StructType)
				if !isStruct || st.Fields == nil {
					continue
				}
				for _, fld := range st.Fields.List {
					kind := classify(fld.Type)
					if len(fld.Names) == 0 {
						fields.add(modEmbeddedName(fld.Type), kind)
					}
					for _, id := range fld.Names {
						fields.add(id.Name, kind)
					}
				}
			}
		}
	}
	return kindTables{fields: fields.result(), methods: methods.result()}
}

// kindTablesCache keeps the tables of the modules and files of a run.
type kindTablesCache struct {
	modules map[*moduleIndex]kindTables
	files   map[*goSource]kindTables
}

// of returns the tables of the module of the file, or of the file alone when it
// belongs to none.
func (c *kindTablesCache) of(mf *modFile, g *goSource, classifyType func(modType) uint8, classifyExpr func(ast.Expr) uint8) kindTables {
	if mf != nil {
		if t, built := c.modules[mf.ix]; built {
			return t
		}
		if c.modules == nil {
			c.modules = map[*moduleIndex]kindTables{}
		}
		t := moduleKindTables(mf.ix, classifyType)
		c.modules[mf.ix] = t
		return t
	}

	if t, built := c.files[g]; built {
		return t
	}
	if c.files == nil {
		c.files = map[*goSource]kindTables{}
	}
	t := fileKindTables(g.file, classifyExpr)
	c.files[g] = t
	return t
}
