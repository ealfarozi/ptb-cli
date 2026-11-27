// Package ui renders terminal output: the PTB banner, colors, tables and the
// machine formats (JSON, CSV, NDJSON). Decoration only ever goes to a TTY;
// structured output on stdout is always clean.
package ui

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// Banner is the canonical straight PTB logo (assets/banner/ptb-straight.txt).
var Banner = []string{
	"██████    ████████   ██████",
	"██   ██      ██      ██   ██",
	"██████       ██      ██████",
	"██           ██      ██   ██",
	"██           ██      ██   ██",
	"██           ██      ██████",
}

// BannerASCII is the fallback when block glyphs are not wanted.
var BannerASCII = []string{
	"PPPPPP    TTTTTTTT   BBBBBB",
	"PP   PP      TT      BB   BB",
	"PPPPPP       TT      BBBBBB",
	"PP           TT      BB   BB",
	"PP           TT      BB   BB",
	"PP           TT      BBBBBB",
}

// Settings decided once per run from flags, config and environment.
type Settings struct {
	Color      bool
	Animations bool
	Banner     bool
	Plain      bool
	Light      bool
	Quiet      bool
}

var S = Settings{}

// IsTTY reports whether f is an interactive terminal.
func IsTTY(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	violet = "\x1b[38;5;141m"
	cyan   = "\x1b[38;5;87m"
	green  = "\x1b[38;5;78m"
	red    = "\x1b[38;5;203m"
	yellow = "\x1b[38;5;221m"
)

func paint(code, s string) string {
	if !S.Color {
		return s
	}
	if S.Light && code == violet {
		code = "\x1b[38;5;92m"
	}
	return code + s + reset
}

func Bold(s string) string  { return paint(bold, s) }
func Dim(s string) string   { return paint(dim, s) }
func Brand(s string) string { return paint(violet, s) }
func Info(s string) string  { return paint(cyan, s) }
func Good(s string) string  { return paint(green, s) }
func Bad(s string) string   { return paint(red, s) }
func Warn(s string) string  { return paint(yellow, s) }

// Status prefixes keep meaning without color (accessibility).
func OK(msg string) string      { return Good("✓ OK") + "  " + msg }
func Fail(msg string) string    { return Bad("✗ GAGAL") + "  " + msg }
func Caution(msg string) string { return Warn("! PERHATIAN") + "  " + msg }

// Errf prints a line on stderr (never on stdout).
func Errf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

// Notef prints an informational line on stderr unless --quiet.
func Notef(format string, a ...interface{}) {
	if S.Quiet {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

// PrintBanner writes the banner, welcome and actual version to w. With
// animations it first resolves seeded particles onto the exact glyph grid
// (the final geometry never changes).
func PrintBanner(w io.Writer, version, channel string) {
	lines := Banner
	if S.Plain {
		lines = BannerASCII
	}
	if S.Animations && !S.Plain {
		animate(w, lines)
	}
	for _, l := range lines {
		fmt.Fprintln(w, Brand(l))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, Bold("Welcome to PaperToBillion CLI"))
	sep := " • "
	if S.Plain {
		sep = " - "
	}
	fmt.Fprintln(w, Dim("Version "+version+sep+channel))
}

func animate(w io.Writer, lines []string) {
	grid := make([][]rune, len(lines))
	for i, l := range lines {
		grid[i] = []rune(l)
	}
	r := rand.New(rand.NewSource(42))
	frames := 8
	glyphs := []rune{'·', '+', '█'}
	fmt.Fprint(w, "\x1b[?25l") // hide cursor
	for f := 0; f <= frames; f++ {
		var b strings.Builder
		for _, row := range grid {
			for _, ch := range row {
				switch {
				case ch == ' ':
					b.WriteRune(' ')
				case f == frames || r.Intn(frames) < f:
					b.WriteRune(ch)
				default:
					b.WriteRune(glyphs[r.Intn(len(glyphs))])
				}
			}
			b.WriteRune('\n')
		}
		fmt.Fprint(w, Brand(b.String()))
		time.Sleep(110 * time.Millisecond)
		fmt.Fprintf(w, "\x1b[%dA", len(grid)) // back to the top of the logo
	}
	fmt.Fprint(w, "\x1b[?25h")
}

// ─── Structured output ───────────────────────────────────────────────────────

// Format is the output format.
type Format string

const (
	Table  Format = "table"
	JSON   Format = "json"
	CSV    Format = "csv"
	NDJSON Format = "ndjson"
)

func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case Table, JSON, CSV, NDJSON:
		return Format(s), nil
	}
	return "", fmt.Errorf("format output tidak dikenal %q: pilih table, json, csv, atau ndjson", s)
}

// Render writes data (the API's data field) in the chosen format.
func Render(w io.Writer, data json.RawMessage, f Format) error {
	switch f {
	case JSON:
		var buf bytes.Buffer
		if err := json.Indent(&buf, data, "", "  "); err != nil {
			_, err = w.Write(data)
			return err
		}
		buf.WriteByte('\n')
		_, err := w.Write(buf.Bytes())
		return err
	case NDJSON:
		rows, _, _ := rowsOf(data)
		if rows == nil {
			var v interface{}
			_ = json.Unmarshal(data, &v)
			rows = []interface{}{v}
		}
		enc := json.NewEncoder(w)
		for _, r := range rows {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	case CSV:
		rows, _, _ := rowsOf(data)
		if rows == nil {
			var v interface{}
			_ = json.Unmarshal(data, &v)
			rows = []interface{}{v}
		}
		cols := columns(rows)
		cw := csv.NewWriter(w)
		_ = cw.Write(cols)
		for _, r := range rows {
			rec := make([]string, len(cols))
			if m, ok := r.(map[string]interface{}); ok {
				for i, c := range cols {
					rec[i] = cell(m[c], false)
				}
			} else if len(cols) == 1 {
				rec[0] = cell(r, false)
			}
			_ = cw.Write(rec)
		}
		cw.Flush()
		return cw.Error()
	}
	return renderTable(w, data)
}

// rowsOf finds the main list of a response: the data itself when it is an
// array, else its largest array-of-objects field. It also returns the scalar
// fields (shown above the table) and the list's name.
func rowsOf(data json.RawMessage) ([]interface{}, map[string]interface{}, string) {
	var v interface{}
	if json.Unmarshal(data, &v) != nil {
		return nil, nil, ""
	}
	if arr, ok := v.([]interface{}); ok {
		return arr, nil, ""
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		return nil, nil, ""
	}
	best, bestKey := []interface{}(nil), ""
	for k, val := range obj {
		if arr, ok := val.([]interface{}); ok && len(arr) > len(best) {
			best, bestKey = arr, k
		}
	}
	scalars := map[string]interface{}{}
	for k, val := range obj {
		if k == bestKey || k == "billing" {
			continue
		}
		switch val.(type) {
		case []interface{}:
		default:
			scalars[k] = val
		}
	}
	return best, scalars, bestKey
}

func columns(rows []interface{}) []string {
	seen := map[string]bool{}
	var cols []string
	for _, r := range rows {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
		if len(seen) > 0 && len(rows) > 50 {
			break // first rows decide the columns for long lists
		}
	}
	if len(cols) == 0 {
		cols = []string{"value"}
	}
	return cols
}

func cell(v interface{}, human bool) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) && (t < 1e15 && t > -1e15) {
			if human {
				return groupInt(int64(t))
			}
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func groupInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func renderTable(w io.Writer, data json.RawMessage) error {
	rows, scalars, name := rowsOf(data)
	if len(scalars) > 0 {
		keys := make([]string, 0, len(scalars))
		for k := range scalars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		width := 0
		for _, k := range keys {
			if len(k) > width {
				width = len(k)
			}
		}
		for _, k := range keys {
			fmt.Fprintf(w, "%s  %s\n", Dim(fmt.Sprintf("%-*s", width, k)), truncate(cell(scalars[k], true), 100))
		}
		if len(rows) > 0 {
			fmt.Fprintln(w)
		}
	}
	if rows == nil {
		if len(scalars) == 0 {
			var buf bytes.Buffer
			_ = json.Indent(&buf, data, "", "  ")
			fmt.Fprintln(w, buf.String())
		}
		return nil
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, Dim("(tidak ada baris)"))
		return nil
	}
	cols := columns(rows)
	if name != "" {
		fmt.Fprintln(w, Bold(name)+Dim(fmt.Sprintf(" (%d)", len(rows))))
	}
	table := make([][]string, 0, len(rows))
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = utf8.RuneCountInString(label(c))
	}
	for _, r := range rows {
		rec := make([]string, len(cols))
		if m, ok := r.(map[string]interface{}); ok {
			for i, c := range cols {
				rec[i] = truncate(cell(m[c], true), 40)
			}
		} else {
			rec[0] = truncate(cell(r, true), 60)
		}
		for i, s := range rec {
			if n := utf8.RuneCountInString(s); n > widths[i] {
				widths[i] = n
			}
		}
		table = append(table, rec)
	}
	head := make([]string, len(cols))
	for i, c := range cols {
		head[i] = pad(label(c), widths[i])
	}
	fmt.Fprintln(w, Info(strings.Join(head, "  ")))
	for _, rec := range table {
		parts := make([]string, len(rec))
		for i, s := range rec {
			parts[i] = pad(s, widths[i])
		}
		fmt.Fprintln(w, strings.TrimRight(strings.Join(parts, "  "), " "))
	}
	return nil
}

// label drops an ordering prefix ("1 perintah" → "perintah") from a column key.
func label(c string) string {
	if i := strings.IndexByte(c, ' '); i > 0 && i <= 2 {
		if _, err := strconv.Atoi(c[:i]); err == nil {
			return c[i+1:]
		}
	}
	return c
}

func pad(s string, n int) string {
	if d := n - utf8.RuneCountInString(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// Group formats an integer with dots (Indonesian style).
func Group(n int64) string { return groupInt(n) }
