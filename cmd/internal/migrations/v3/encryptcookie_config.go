package v3

import (
	"encoding/base64"
	"fmt"
	"go/ast"
	"slices"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var mw1Encryptcookie = newMW1Package("encryptcookie")

// MigrateEncryptcookieConfig moves the encryptcookie middleware to v3:
//
//   - The Encryptor and Decryptor of the config get the cookie name as their
//     first argument: a function literal gets the parameter, any other function
//     is wrapped. The stock functions are left alone, they have the signature
//     already.
//   - EncryptCookie and DecryptCookie take the cookie name first as well. The
//     name binds a value to its cookie in v3; an empty one reads and writes what
//     v2 did, so the values that were stored with v2 stay readable.
//   - GenerateKey takes the length of the key: 32 bytes, what v2 generated.
func MigrateEncryptcookieConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	idx := newMW1Index(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateEncryptcookieContent(content, fi, idx)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate encryptcookie configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating encryptcookie middleware configs")
	return nil
}

// The hooks of encryptcookie.Config and their parameters in v2.
const (
	encryptcookieEncryptor = "Encryptor"
	encryptcookieDecryptor = "Decryptor"
	encryptcookieHookArgs  = 2
	// encryptcookieKeyBytes is the length of the keys of v2.
	encryptcookieKeyBytes = "32"
)

// encryptcookieMigration is the state of the rewrite of one file.
type encryptcookieMigration struct {
	g     *goSource
	rep   *mw1Reporter
	idx   *mw1Index
	edits editSet
}

func migrateEncryptcookieContent(content string, fi internal.FileInfo, idx *mw1Index) string {
	names := mw1Qualifiers(mw1Encryptcookie.names(content))
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	m := &encryptcookieMigration{g: g, rep: newMW1Reporter(g, fi), idx: idx}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if lit, isConfig := mw1ConfigLit(x, names); isConfig {
				m.config(lit, names)
			}
		case *ast.CallExpr:
			m.call(x, names)
		case *ast.AssignStmt:
			if _, field, isAssign := mw1AssignedField(x, stack, names); isAssign && isHook(field) {
				m.hook(x.Rhs[0], field, names)
			}
		}
		return true
	})

	out := m.edits.apply(content)
	m.rep.flush(&m.edits, out, out)
	return out
}

func isHook(field string) bool {
	return field == encryptcookieEncryptor || field == encryptcookieDecryptor
}

// config rewrites the hooks of an encryptcookie.Config literal.
func (m *encryptcookieMigration) config(lit *ast.CompositeLit, names []string) {
	custom := false
	for _, name := range []string{encryptcookieEncryptor, encryptcookieDecryptor} {
		if kv := mw1KeyValue(lit, name); kv != nil {
			custom = true
			m.hook(kv.Value, name, names)
		}
	}

	// v3 checks the key when the middleware is created, hooks or not; v2 only
	// needed it for the stock functions
	if kv := mw1KeyValue(lit, "Key"); custom && kv != nil {
		if key, isLiteral := mw1StringLiteral(kv.Value); isLiteral && !validCookieKey(key) {
			m.rep.notef(kv, "v3 panics at start-up unless the Key is the base64 encoding of 16, 24 or 32 bytes, even with your own Encryptor and Decryptor: encryptcookie.GenerateKey(32) makes one")
		}
	}
}

// validCookieKey reports whether the key is one that v3 accepts.
func validCookieKey(key string) bool {
	decoded, err := base64.StdEncoding.DecodeString(key)
	return err == nil && slices.Contains([]int{16, 24, 32}, len(decoded))
}

// hook gives an Encryptor or a Decryptor the name of the cookie as a first
// parameter.
func (m *encryptcookieMigration) hook(e ast.Expr, field string, names []string) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		m.hook(x.X, field, names)
	case *ast.FuncLit:
		if mw1ParamCount(x.Type) != encryptcookieHookArgs {
			return
		}
		offset, text := mw1PrependParam(m.g, x.Type, "string")
		m.edits.replace(offset, offset, text)
	case *ast.Ident:
		if x.Name != mw1NilIdent {
			m.wrap(e)
		}
	case *ast.SelectorExpr:
		// the stock functions already take the name
		if mw1IsMember(x, names, "EncryptCookie") || mw1IsMember(x, names, "DecryptCookie") {
			return
		}
		m.wrap(e)
	default:
		m.rep.notef(e, "the %s of encryptcookie.Config takes the name of the cookie as a first argument in v3: wrap %s into a func(name, value, key string) (string, error)", field, m.g.text(e))
	}
}

// wrap replaces a function value with a function literal that calls it,
// unless the file shows that the function takes the name already.
func (m *encryptcookieMigration) wrap(e ast.Expr) {
	if mw1DeclaredWithParams(m.g.file, m.idx, e, encryptcookieHookArgs+1) {
		return
	}

	value, key := mw1FreeNames(e, "value", "key")
	m.edits.replaceNode(m.g, e, fmt.Sprintf("func(_, %s, %s string) (string, error) { return %s(%s, %s) }",
		value, key, m.g.text(e), value, key))
}

// call migrates the calls of the functions of the package whose signature
// changed.
func (m *encryptcookieMigration) call(call *ast.CallExpr, names []string) {
	switch {
	case mw1IsMember(call.Fun, names, "GenerateKey") && len(call.Args) == 0:
		m.edits.replace(m.g.offset(call.Rparen), m.g.offset(call.Rparen), encryptcookieKeyBytes)
	case (mw1IsMember(call.Fun, names, "EncryptCookie") || mw1IsMember(call.Fun, names, "DecryptCookie")) &&
		len(call.Args) == encryptcookieHookArgs && !call.Ellipsis.IsValid():
		first := m.g.offset(call.Args[0].Pos())
		m.edits.replace(first, first, `"", `)
	}
}
