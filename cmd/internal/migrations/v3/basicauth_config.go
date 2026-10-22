package v3

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"go/ast"
	"regexp"
	"strconv"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// basicauthHashed matches the passwords that v3 reads as a hash: the explicit
// {SHA256} and {SHA512} forms and bcrypt.
var basicauthHashed = regexp.MustCompile(`^(\{SHA(256|512)\}|\$2[abxy]?\$\d\d\$)`)

// MigrateBasicauthConfig brings basicauth.Config to v3:
//
//   - ContextUsername and ContextPassword are removed: the username is read with
//     basicauth.UsernameFromContext and the password is not kept at all.
//   - Users must hold hashed passwords. Plaintext literals (and constants) are
//     hashed with SHA-256 from their value, and a map that is not a literal
//     (a variable, values from the environment) is hashed when the middleware
//     is created, since its content is not known here.
//
// Users is left alone next to an Authorizer, which both versions use instead of
// it.
func MigrateBasicauthConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateBasicauthConfigContent)
	if err != nil {
		return fmt.Errorf("failed to migrate basicauth config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating basicauth configs")
	return nil
}

// The fields of basicauth.Config that v3 removed.
const (
	basicauthContextUsername = "ContextUsername"
	basicauthContextPassword = "ContextPassword"
)

// basicauthMigration is the state of the rewrite of one file.
type basicauthMigration struct {
	g           *goSource
	fi          internal.FileInfo
	consts      map[string]string
	sha256Name  string
	base64Name  string
	edits       editSet
	needsSHA256 bool
	needsBase64 bool
	usesSHA     bool
}

func migrateBasicauthConfigContent(content string, fi internal.FileInfo) string {
	names := mw1Qualifiers(mw1Basicauth.names(content))
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	m := &basicauthMigration{g: g, fi: fi, consts: mw1StringConsts(g.file)}
	m.sha256Name, m.needsSHA256 = mw1ImportName(content, "crypto/sha256", "sha256")
	m.base64Name, m.needsBase64 = mw1ImportName(content, "encoding/base64", "base64")

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if lit, isConfig := mw1ConfigLit(n, names); isConfig {
			m.literal(lit)
		} else if as, field, isAssign := mw1AssignedField(n, stack, names); isAssign {
			m.assignment(as, field, stack)
		}
		return true
	})

	out := m.edits.apply(content)
	if m.usesSHA {
		if m.needsSHA256 {
			out = addImport(out, "crypto/sha256")
		}
		if m.needsBase64 {
			out = addImport(out, "encoding/base64")
		}
	}
	return out
}

// literal rewrites the fields of a basicauth.Config literal.
func (m *basicauthMigration) literal(lit *ast.CompositeLit) {
	authorizer := mw1KeyValue(lit, "Authorizer")
	for i, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent {
			continue
		}

		switch key.Name {
		case basicauthContextUsername:
			start, end := mw1ElementSpan(m.g, lit, i)
			m.edits.replace(start, end, "")
		case basicauthContextPassword:
			start, end := mw1ElementSpan(m.g, lit, i)
			m.edits.replace(start, end, "")
			m.reportPassword(kv)
		case "Users":
			if authorizer == nil {
				m.users(kv.Value)
			}
		}
	}
}

// assignment rewrites `cfg.Field = value` on a variable that holds a
// basicauth.Config.
func (m *basicauthMigration) assignment(as *ast.AssignStmt, field string, stack []ast.Node) {
	switch field {
	case "Users":
		m.users(as.Rhs[0])
	case basicauthContextUsername, basicauthContextPassword:
		if field == basicauthContextPassword {
			m.reportPassword(as)
		}
		if len(stack) > 0 && m.isLineStatement(as, stack) {
			start := m.g.offset(as.Pos())
			end := m.g.offset(as.End())
			lineStart := start - len(mw1Indent(m.g.src, start))
			if end < len(m.g.src) && m.g.src[end] == '\n' {
				end++
			}
			m.edits.replace(lineStart, end, "")
			return
		}
		internal.Notef(m.fi, m.g.line(as.Pos()),
			"basicauth.Config has no %s in v3: remove this assignment (the username is read with basicauth.UsernameFromContext(c))", field)
	}
}

// isLineStatement reports whether the assignment is a statement of a block that
// takes its line on its own.
func (m *basicauthMigration) isLineStatement(as *ast.AssignStmt, stack []ast.Node) bool {
	switch stack[len(stack)-1].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
	default:
		return false
	}
	start := m.g.offset(as.Pos())
	end := mw1SkipSpaces(m.g.src, m.g.offset(as.End()))
	return mw1StartsLine(m.g.src, start) && (end >= len(m.g.src) || m.g.src[end] == '\n')
}

func (m *basicauthMigration) reportPassword(at ast.Node) {
	internal.Notef(m.fi, m.g.line(at.Pos()),
		"basicauth no longer stores the password in the request context, so ContextPassword was removed: "+
			"if the application reads it back, keep it from the Authorizer(user, pass string, c fiber.Ctx)")
}

// users rewrites the value of Users.
func (m *basicauthMigration) users(e ast.Expr) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		m.users(x.X)
	case *ast.CompositeLit:
		m.literalUsers(x)
	case *ast.CallExpr:
		// a function literal called on the map is the hashing that was added before
		if _, isFunc := x.Fun.(*ast.FuncLit); !isFunc {
			m.hashAtStartup(e)
		}
	case *ast.Ident:
		if x.Name != mw1NilIdent {
			m.hashAtStartup(e)
		}
	default:
		m.hashAtStartup(e)
	}
}

// literalUsers hashes the passwords of a map literal whose values are known,
// and wraps the whole map into the hashing at start-up when one is not.
func (m *basicauthMigration) literalUsers(lit *ast.CompositeLit) {
	var hashes editSet
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			m.hashAtStartup(lit)
			return
		}
		password, known := m.password(kv.Value)
		if !known {
			m.hashAtStartup(lit)
			return
		}
		if basicauthHashed.MatchString(password) {
			continue
		}
		hashes.replaceNode(m.g, kv.Value, strconv.Quote(sha256Password(password)))
	}
	m.edits.edits = append(m.edits.edits, hashes.edits...)
}

// password returns the value of a password literal or of a constant.
func (m *basicauthMigration) password(e ast.Expr) (string, bool) {
	if s, ok := mw1StringLiteral(e); ok {
		return s, true
	}
	if id, ok := e.(*ast.Ident); ok {
		s, found := m.consts[id.Name]
		return s, found
	}
	return "", false
}

// sha256Password renders the {SHA256} form of a password.
func sha256Password(password string) string {
	sum := sha256.Sum256([]byte(password))
	return "{SHA256}" + base64.StdEncoding.EncodeToString(sum[:])
}

// hashAtStartup wraps a map expression into the function that hashes its
// passwords, which keeps working whatever the map holds and wherever it comes
// from.
func (m *basicauthMigration) hashAtStartup(e ast.Expr) {
	m.usesSHA = true
	m.edits.replaceNode(m.g, e, fmt.Sprintf(`func(plain map[string]string) map[string]string {
	// v3 only accepts hashed passwords: they are hashed here, when the middleware is created
	hashed := make(map[string]string, len(plain))
	for user, pass := range plain {
		sum := %[1]s.Sum256([]byte(pass))
		hashed[user] = "{SHA256}" + %[2]s.StdEncoding.EncodeToString(sum[:])
	}
	return hashed
}(%[3]s)`, m.sha256Name, m.base64Name, m.g.text(e)))
}
