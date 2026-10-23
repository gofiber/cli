// Package gettershim is the source of getters.go of the package
// <module>/internal/fibermigrate, the file that "fiber migrate" adds to a module that
// calls ParamsInt or QueryFloat of the context of Fiber v2 where the helpers of Fiber v3
// do not do the same: the error of ParamsInt read "failed to convert: " before the one of
// strconv.Atoi, which the call of strconv does not say, and QueryFloat read every number
// that strconv.ParseFloat reads, where fiber.Query[float64] reads decimal numbers only.
//
// The package exists so that the code is built, vetted, linted and tested together
// with the CLI. The migration embeds getters.go and writes everything below the
// package clause into the module, with the package clause replaced. The file must
// compile on its own and import nothing but the standard library and Fiber, and its
// unexported top level names start with getter, because the files of the other
// migrations share the package (module_shim_collision_internal_test.go checks it).
package gettershim

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v3"
)

// ParamsInt returns the route parameter as an int: the first default and no error when
// it is no integer and there is a default, 0 and an error that reads "failed to convert: "
// and the error of strconv.Atoi when there is none. It is Ctx.ParamsInt of Fiber v2.
func ParamsInt(c fiber.Ctx, key string, defaultValue ...int) (int, error) {
	value, err := strconv.Atoi(c.Params(key))
	if err != nil {
		if len(defaultValue) > 0 {
			return defaultValue[0], nil
		}
		return 0, fmt.Errorf("failed to convert: %w", err)
	}
	return value, nil
}

// QueryFloat returns the query parameter as a float64: the first default, or 0 when there
// is none, if it is no number that strconv.ParseFloat reads. It is Ctx.QueryFloat of Fiber
// v2, which reads what fiber.Query[float64] of Fiber v3 does not: inf, infinity and nan in
// any case, hexadecimal numbers such as 0x1p-2, underscores between the digits as in
// 1_000.5, and the numbers at the ends of the range of a float64.
func QueryFloat(c fiber.Ctx, key string, defaultValue ...float64) float64 {
	value, err := strconv.ParseFloat(c.Query(key), 64)
	if err != nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return value
}
