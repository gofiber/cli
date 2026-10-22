package internal

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_usesCRLF(t *testing.T) {
	t.Parallel()

	assert.False(t, usesCRLF(""))
	assert.False(t, usesCRLF("a\nb\n"))
	assert.True(t, usesCRLF("a\r\nb\r\n"))
	assert.True(t, usesCRLF("a\r\nb\r\nc\n"), "most lines decide")
	assert.False(t, usesCRLF("a\r\nb\nc\n"))
	assert.False(t, usesCRLF("a\rb"), "a lone carriage return is no line ending")
}

func Test_lineEndingsRoundTrip(t *testing.T) {
	t.Parallel()

	src := "package a\r\n\r\nvar s = `x\r\ny`\r\n"
	assert.Equal(t, "package a\n\nvar s = `x\ny`\n", toLF(src))
	assert.Equal(t, src, toCRLF(toLF(src)))
}

// letters are the lines of the test texts: few of them, so that many repeat, and those of the changed text are upper case.
const letters = "abcdefABCDEF"

// randomLines returns n lines chosen from a small alphabet, so that many of them repeat.
func randomLines(rng *rand.Rand, n int) []string {
	lines := make([]string, n)
	for i := range lines {
		pick := rng.IntN(6)
		lines[i] = letters[pick : pick+1]
	}
	return lines
}

// changedLine returns a line that no randomLines produces.
func changedLine(rng *rand.Rand) string {
	pick := 6 + rng.IntN(6)
	return letters[pick : pick+1]
}

// mutate returns a copy of lines with a few lines changed, added or removed.
func mutate(rng *rand.Rand, lines []string) []string {
	out := append([]string(nil), lines...)
	for range rng.IntN(5) {
		switch pos := rng.IntN(len(out) + 1); rng.IntN(3) {
		case 0:
			out = append(out[:pos], append([]string{changedLine(rng)}, out[pos:]...)...)
		case 1:
			if pos < len(out) {
				out = append(out[:pos], out[pos+1:]...)
			}
		default:
			if pos < len(out) {
				out[pos] = changedLine(rng)
			}
		}
	}
	return out
}

func Test_diffLines_HunksTurnAIntoB(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a seeded generator of test data
	for range 3000 {
		a := randomLines(rng, rng.IntN(25))
		b := mutate(rng, a)
		if rng.IntN(4) == 0 {
			b = randomLines(rng, rng.IntN(25))
		}

		hunks, ok := diffLines(a, b)
		require.True(t, ok)
		require.True(t, slices.Equal(b, applyHunks(a, b, hunks, 0, len(a))), "a: %v b: %v hunks: %v", a, b, hunks)

		end := 0
		for _, h := range hunks {
			require.GreaterOrEqual(t, h.aStart, end, "hunks are in order and apart: %v", hunks)
			require.False(t, h.aStart == h.aEnd && h.bStart == h.bEnd, "an empty hunk: %v", hunks)
			end = h.aEnd
		}
	}
}

func Test_diffLines_TooManyEdits(t *testing.T) {
	t.Parallel()

	a := make([]string, 3*maxDiffEdits)
	b := make([]string, 3*maxDiffEdits)
	for i := range a {
		a[i] = "a" + strings.Repeat("x", i%7)
		b[i] = "b" + strings.Repeat("y", i%5)
	}
	_, ok := diffLines(a, b)
	assert.False(t, ok)
}

func Test_merge3_Properties(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // a seeded generator of test data
	for range 3000 {
		base := randomLines(rng, rng.IntN(25))
		changed := mutate(rng, base)

		got, ok := merge3(base, base, changed)
		require.True(t, ok)
		require.True(t, slices.Equal(changed, got), "only theirs changed: base %v theirs %v got %v", base, changed, got)

		got, ok = merge3(base, changed, base)
		require.True(t, ok)
		require.True(t, slices.Equal(changed, got), "only ours changed: base %v ours %v got %v", base, changed, got)

		other := mutate(rng, base)
		got, ok = merge3(base, changed, other)
		require.True(t, ok)
		// every line of the migrated text that is not in the base stays, however the author changed things
		for _, line := range other {
			if !strings.Contains(strings.Join(base, "|"), line) {
				require.Contains(t, got, line, "base %v ours %v theirs %v", base, changed, other)
			}
		}
	}
}

func Test_merge3_ChangesInSeparateHalvesAreBothKept(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(5, 6)) //nolint:gosec // a seeded generator of test data
	for range 3000 {
		// the halves are apart by a line that nobody changes
		left, right := randomLines(rng, 1+rng.IntN(12)), randomLines(rng, 1+rng.IntN(12))
		base := slices.Concat(left, []string{"--"}, right)
		oursLeft, theirsRight := mutate(rng, left), mutate(rng, right)
		ours := slices.Concat(oursLeft, []string{"--"}, right)
		theirs := slices.Concat(left, []string{"--"}, theirsRight)

		got, ok := merge3(base, ours, theirs)
		require.True(t, ok)
		want := slices.Concat(oursLeft, []string{"--"}, theirsRight)
		require.True(t, slices.Equal(want, got), "base %v ours %v theirs %v got %v want %v", base, ours, theirs, got, want)
	}
}

func Test_merge3_DisjointChangesAreBothKept(t *testing.T) {
	t.Parallel()

	base := []string{"1", "2", "3", "4", "5", "6", "7"}
	ours := []string{"1", "TWO", "3", "4", "5", "6", "7"}
	theirs := []string{"1", "2", "3", "4", "5", "SIX", "7"}

	got, ok := merge3(base, ours, theirs)
	require.True(t, ok)
	assert.Equal(t, []string{"1", "TWO", "3", "4", "5", "SIX", "7"}, got)
}

func Test_merge3_NeighboringLinesAreBothKept(t *testing.T) {
	t.Parallel()

	base := []string{"1", "2", "3", "4"}
	ours := []string{"1", "TWO", "3", "4"}
	theirs := []string{"1", "2", "THREE", "4"}

	got, ok := merge3(base, ours, theirs)
	require.True(t, ok)
	assert.Equal(t, []string{"1", "TWO", "THREE", "4"}, got, "the formatter lays out a line by itself, so the author's line next to a migrated one stays")
}

func Test_merge3_TheSameLinesTakeTheMigration(t *testing.T) {
	t.Parallel()

	base := []string{"1", "2", "3", "4"}
	ours := []string{"1", "TWO", "3", "4"}
	theirs := []string{"1", "ZWEI", "3", "4"}

	got, ok := merge3(base, ours, theirs)
	require.True(t, ok)
	assert.Equal(t, []string{"1", "ZWEI", "3", "4"}, got)

	// two insertions at one place are in conflict too
	got, ok = merge3([]string{"1", "2"}, []string{"1", "mine", "2"}, []string{"1", "theirs", "2"})
	require.True(t, ok)
	assert.Equal(t, []string{"1", "theirs", "2"}, got)
}

func tidyFor(t *testing.T) func(string) (string, error) {
	t.Helper()
	return func(src string) (string, error) {
		b, err := tidyImports("x.go", []byte(src))
		return string(b), err
	}
}

func formatFor(t *testing.T) func(string) (string, error) {
	t.Helper()
	return func(src string) (string, error) {
		b, err := formatOnly("x.go", []byte(src))
		return string(b), err
	}
}

func Test_keepFormatting_KeepsTheAuthorsLinesApartFromTheMigration(t *testing.T) {
	t.Parallel()

	original := `package main

import "github.com/gofiber/fiber/v2"

//Hello says hello.
func Hello(c *fiber.Ctx) error {
	x := map[string]int{
		"a":   1,
		"bbb": 2,
	}
	_ = x

	return c.SendString("hello")
}

//Bye says bye.
func Bye(c *fiber.Ctx) error {
	return c.SendString("bye")
}
`
	// the migration changes the handler signatures only
	processed := strings.ReplaceAll(strings.ReplaceAll(original, "*fiber.Ctx", "fiber.Ctx"), "fiber/v2", "fiber/v3")
	formatted, err := tidyFor(t)(processed)
	require.NoError(t, err)
	require.Contains(t, formatted, "// Hello says hello.", "the formatter fixes the comments")

	got := keepFormatting(original, formatted, formatFor(t))

	want := strings.ReplaceAll(strings.ReplaceAll(original, "*fiber.Ctx", "fiber.Ctx"), "fiber/v2", "fiber/v3")
	assert.Equal(t, want, got, "the comments the author wrote without a space stay as they were")
}

func Test_keepFormatting_FormattedOriginalIsTheFormattedText(t *testing.T) {
	t.Parallel()

	original := "package main\n\nvar x = 1\n"
	formatted := "package main\n\nvar x = 2\n"
	assert.Equal(t, formatted, keepFormatting(original, formatted, formatFor(t)))
}

func Test_keepFormatting_NeverReturnsSomethingThatDiffersFromTheFormattedText(t *testing.T) {
	t.Parallel()

	// the formatter would tidy the merged text to something else: the formatted text is used
	original := "package main\n\nvar a  = 1\nvar b = 2\n"
	formatted := "package main\n\nvar a = 1\nvar b = 3\n"
	format := func(src string) (string, error) {
		if src == original {
			return "package main\n\nvar a = 1\nvar b = 2\n", nil
		}
		return "something else", nil
	}
	got := keepFormatting(original, formatted, format)
	assert.Equal(t, formatted, got)
}
