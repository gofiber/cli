package v3

import (
	"fmt"
	"go/ast"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// contribWebsocketMentionRe finds the files that import the websocket middleware.
var contribWebsocketMentionRe = regexp.MustCompile(`github\.com/gofiber/(?:contrib/(?:v3/)?websocket|websocket)\b`)

// MigrateWebsocketConfig turns the Filter of the websocket Config into the Next
// of v3.
//
// The two are opposites. The comment of the v2 field says that it skips the
// middleware, but the code handled a request only when Filter returned true;
// Next skips the middleware when it returns true. Renaming the field would turn
// the middleware inside out, so the function is negated: Filter: F becomes
// Next: func(c fiber.Ctx) bool { return !F(c) }.
func MigrateWebsocketConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateWebsocketContent)
	if err != nil {
		return fmt.Errorf("failed to migrate websocket config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating websocket configs")
	return nil
}

func migrateWebsocketContent(content string, fi internal.FileInfo) string {
	if !contribWebsocketMentionRe.MatchString(content) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}
	names := contribNames(g, "websocket")
	if len(names) == 0 {
		return content
	}

	f := newContribFile(g, fi)
	for _, c := range f.findConfigs(names) {
		f.websocketConfig(c)
	}
	return f.apply()
}

// websocketConfig migrates one websocket.Config.
func (f *contribFile) websocketConfig(c *contribConfig) {
	defer c.finish()

	// A config that has its Next is a v3 config already.
	filters := c.slotsOf("Filter")
	if len(filters) == 0 || c.get("Next") != nil {
		return
	}

	for _, s := range filters {
		f.negateFilter(s.value)
	}
	c.rename("Filter", "Next")
}

// negateFilter schedules the change that makes the function e answer the opposite.
func (f *contribFile) negateFilter(e ast.Expr) {
	if contribIsNil(e) {
		return
	}

	// func(c *fiber.Ctx) bool { return X } only needs its X changed.
	if p, ok := contribPredicateOf(e); ok {
		f.edits.replaceNode(f.g, p.ret.Results[0], contribNegate(f.g, p.ret.Results[0]))
		return
	}

	param := contribFreeName(e, "c", "ctx", "fctx")
	f.edits.replaceNode(f.g, e, "func("+param+" "+f.fiberName()+".Ctx) bool { return !"+contribCallText(f.g, e)+"("+param+") }")
}
