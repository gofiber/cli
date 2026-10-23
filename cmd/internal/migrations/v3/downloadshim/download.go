// Package downloadshim is the source of download.go of the package
// <module>/internal/fibermigrate, the file that "fiber migrate" adds to a module that
// calls Ctx.Download: Fiber v3 does not serve the byte ranges that Download served in
// Fiber v2, and has no setting to ask for them.
//
// The package exists so that the code is built, vetted, linted and tested together
// with the CLI. The migration embeds download.go and writes everything below the
// package clause into the module, with the package clause replaced. The file must
// compile on its own and import nothing but Fiber, and its unexported top level names
// start with download, because the files of the other migrations share the package
// (module_shim_collision_internal_test.go checks it).
package downloadshim

import (
	"github.com/gofiber/fiber/v3"
)

// Download sends the file as an attachment under the given name, which is the file
// itself when none is given. It is Ctx.Download of Fiber v3, with the byte ranges
// that Ctx.Download of Fiber v2 served: a request for a part of the file is answered
// with that part.
func Download(c fiber.Ctx, file string, filename ...string) error {
	name := file
	if len(filename) > 0 {
		name = filename[0]
	}

	// Attachment names the file in Content-Disposition as Download does, and it also
	// sets the Content-Type from the extension of the name. SendFile would keep that
	// type, because it gives the response the type of the file only while it has none,
	// so the type goes back to what it was: none, unless the handler chose one.
	header := &c.Response().Header
	contentType := string(header.ContentType())
	c.Attachment(name)
	if contentType == fiber.MIMETextPlainCharsetUTF8 {
		contentType = ""
	}
	header.SetContentType(contentType)

	return c.SendFile(file, fiber.SendFile{ByteRange: true})
}
