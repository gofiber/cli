// Package sessionshim is the source of session.go of the package
// <module>/internal/fibermigrate, the file that "fiber migrate" adds to a module
// when the program tells the Source type of the session middleware of Fiber v2
// from a plain string (a type switch or a type assertion), or when it cannot say
// string (a file that declares the identifier itself).
//
// The package exists so that the code is built, vetted, linted and tested together
// with the CLI. The migration embeds session.go and writes everything below the
// package clause into the module, with the package clause replaced. The file must
// compile on its own, and its unexported top level names start with sess, because
// the files of the other migrations share the package
// (module_shim_collision_internal_test.go checks it).
package sessionshim

// SessionSource is the type that Fiber v2 gave to the place a session ID is read
// from (session.Source): a string of its own, so that a program can tell it from a
// plain string where it asks for the type.
type SessionSource string

// The places v2 knew, under the names the constants had (session.SourceCookie,
// session.SourceHeader and session.SourceURLQuery).
const (
	SessionSourceCookie   SessionSource = "cookie"
	SessionSourceHeader   SessionSource = "header"
	SessionSourceURLQuery SessionSource = "query"
)
