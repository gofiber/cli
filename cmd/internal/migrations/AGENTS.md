# Migration Guidelines

- Migration functions must print a message only if they performed changes.
- Use `internal.ChangeFileContent` (or `ChangeFileContentInfo` when you need the file, to attach notes), which returns a boolean, and print the message only when it is `true`.
- A migration prints nothing on its second run: keep every rewrite idempotent.

## Writing a rewrite that does not damage other code

- Only touch files that import the package whose API you rewrite, and go through the file's import name (`f "github.com/gofiber/fiber/v3"` is `f.`), never a hard-coded `fiber.` or `cors.`. Use `fiberNames` / `importNames` from `v3/scope.go`.
- Prefer `go/ast` (`parseFile`, `goSource.walk`, `editSet`) over regular expressions. If text scanning is unavoidable it has to be balanced: strings, raw strings, rune literals, comments and nested brackets, and it must stop at the end of the construct it rewrites.
- Never rewrite string literals, comments, or fields and methods of unrelated types, and never assume a package called `session`, `static` or `utils` is Fiber's.
- Add every import the output needs yourself (`addImport`). goimports only finds v3 packages that happen to be in the module cache, so a migration that relies on it works on one machine and not on another.
- Do not drop configuration or user code silently. If there is no faithful v3 equivalent, leave the code in place and record a note (`internal.Notef(fi, line, ...)`); if removing it would change what the program does without a compile error, the old construct must stay so that the build fails.
- A type that embeds `fiber.Ctx` and declares a member of the name your rewrite introduces hides it from calls on the type. Ask `ctxEnv.receiverKindVia` instead of `receiverKind` and put `ctxPlan.qualify` in front of the rewrite, so that the call goes through the embedded field (`c.Ctx.Redirect()`).
- Where an explicit v3 setting reproduces the v2 default (for example `IdleTimeout: 24 * time.Hour`, `ByteRange: true`), set it. Where it would be unsafe, warn instead.

## Notes

Notes are how a migration reports what it could not do. `DoMigration` collects them and prints them once, sorted by file and line. A file that does not parse is left untouched and noted automatically. After the migrations, `v3.Review` records what is left over (v2 imports, identifiers, fields and methods that no longer exist) and the behavior changes of the middleware the project uses; add a rule to `review_behavior.go` for a change that no rewrite can cover.

## Registering and testing

- Register every exported `Migrate*` function in `Migrations` (`lists.go`); a test fails when one is missing. The bands select by the version the project is on and the one it moves to: the v2 to v3 rewrites must not run on a project that is already on v3.
- Tests feed valid v2 code and expect valid v3 code. Cover the aliased import, a multi-line form, a form that must be left alone, and a second run that changes nothing.
