package gettershim

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/fiber/v3"
)

// v2ParamsInt and v2QueryFloat are ParamsInt and QueryFloat of Fiber v2.52, with the
// value of the parameter handed over.
func v2ParamsInt(param string, defaultValue ...int) (int, error) {
	value, err := strconv.Atoi(param)
	if err != nil {
		if len(defaultValue) > 0 {
			return defaultValue[0], nil
		}
		return 0, fmt.Errorf("failed to convert: %w", err)
	}
	return value, nil
}

func v2QueryFloat(param string, defaultValue ...float64) float64 {
	value, err := strconv.ParseFloat(param, 64)
	if err != nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return value
}

// getterNumbers are values that the parsers of Fiber v3 read differently from the ones of v2,
// besides the ones that every parser reads alike.
var getterNumbers = []string{
	"0", "1", "-1", "+1", "007", "+007", "-0", "1_000", "1_000.5", "_1", "1_", "0x1f", "0X1F", "0b101", "0o17",
	"7.0", "7.", ".5", "-.5", "1e3", "1E-3", "1e", " 7", "7 ", "٧", "NaN", "nan", "Inf", "-inf", "+Infinity", "infinity",
	"0x1p-2", "0x1.8p1", "0x1_0p0", "9223372036854775807", "9223372036854775808", "-9223372036854775809", "1e309", "-1e309",
	"4.9e-324", "2.2250738585072014e-308", "1234567890123456789012e2", "abc", "12abc", "true",
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func get(t *testing.T, app *fiber.App, target string) string {
	t.Helper()

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode, target)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

func Test_ParamsInt(t *testing.T) {
	t.Parallel()

	// the escapes of the path are decoded, so that the parameter is the input
	app := fiber.New(fiber.Config{UnescapePath: true})
	app.Get("/n/:n", func(c fiber.Ctx) error {
		without, err := ParamsInt(c, "n")
		with, withErr := ParamsInt(c, "n", -1)
		spread, spreadErr := ParamsInt(c, "n", []int{-2, -3}...)
		return c.SendString(fmt.Sprintf("%d %q|%d %q|%d %q", without, errText(err), with, errText(withErr), spread, errText(spreadErr)))
	})

	for _, in := range getterNumbers {
		want, wantErr := v2ParamsInt(in)
		wantWith, wantWithErr := v2ParamsInt(in, -1)
		wantSpread, wantSpreadErr := v2ParamsInt(in, -2, -3)

		assert.Equal(t,
			fmt.Sprintf("%d %q|%d %q|%d %q", want, errText(wantErr), wantWith, errText(wantWithErr), wantSpread, errText(wantSpreadErr)),
			get(t, app, "/n/"+url.PathEscape(in)), in)
	}
}

func Test_ParamsInt_ErrorText(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/n/:n", func(c fiber.Ctx) error {
		_, err := ParamsInt(c, "n")
		return c.SendString(errText(err))
	})

	assert.Equal(t, `failed to convert: strconv.Atoi: parsing "x": invalid syntax`, get(t, app, "/n/x"))
	assert.Empty(t, get(t, app, "/n/12"))
}

func Test_QueryFloat(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString(fmt.Sprintf("%v|%v|%v", QueryFloat(c, "f"), QueryFloat(c, "f", -1), QueryFloat(c, "f", []float64{-2, -3}...)))
	})

	for _, in := range getterNumbers {
		assert.Equal(t,
			fmt.Sprintf("%v|%v|%v", v2QueryFloat(in), v2QueryFloat(in, -1), v2QueryFloat(in, -2, -3)),
			get(t, app, "/?f="+url.QueryEscape(in)), in)
	}

	// the key is missing
	assert.Equal(t, "0|-1|-2", get(t, app, "/"))
}

func Test_QueryFloat_BeyondDecimalNumbers(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error { return c.SendString(strconv.FormatFloat(QueryFloat(c, "f"), 'g', -1, 64)) })

	for in, want := range map[string]string{
		"1_000.5": "1000.5", "0x1p-2": "0.25", "Inf": "+Inf", "-inf": "-Inf", "nan": "NaN", "4.9e-324": "5e-324",
		"2.2250738585072014e-308": "2.2250738585072014e-308",
	} {
		assert.Equal(t, want, get(t, app, "/?f="+url.QueryEscape(in)), in)
	}
}
