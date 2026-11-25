package ui

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

const sample = `{"symbol":"BBCA","trades":1200,"bars":[{"time":"09:00","delta":5,"c":9100},{"time":"09:05","delta":-2,"c":9125}],"billing":{"chargedTokens":2}}`

func TestStructuredOutputIsCleanAndParses(t *testing.T) {
	S = Settings{Color: true} // even with color on, machine formats stay clean
	for _, f := range []Format{JSON, NDJSON, CSV} {
		var buf bytes.Buffer
		if err := Render(&buf, json.RawMessage(sample), f); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if strings.Contains(out, "\x1b[") {
			t.Fatalf("%s output contains ANSI: %q", f, out)
		}
		switch f {
		case JSON:
			var v map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &v); err != nil || v["symbol"] != "BBCA" {
				t.Fatalf("json: %v %v", err, v)
			}
		case NDJSON:
			lines := strings.Split(strings.TrimSpace(out), "\n")
			if len(lines) != 2 {
				t.Fatalf("ndjson rows: %d", len(lines))
			}
			for _, l := range lines {
				var v map[string]interface{}
				if err := json.Unmarshal([]byte(l), &v); err != nil {
					t.Fatalf("ndjson line %q: %v", l, err)
				}
			}
		case CSV:
			recs, err := csv.NewReader(strings.NewReader(out)).ReadAll()
			if err != nil || len(recs) != 3 || strings.Join(recs[0], ",") != "c,delta,time" || recs[1][0] != "9100" {
				t.Fatalf("csv: %v %v", err, recs)
			}
		}
	}
}

func TestTableShowsScalarsAndRows(t *testing.T) {
	S = Settings{}
	var buf bytes.Buffer
	if err := Render(&buf, json.RawMessage(sample), Table); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"symbol", "BBCA", "1.200", "bars (2)", "09:05", "-2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "chargedTokens") {
		t.Fatal("billing belongs on stderr, not in the table")
	}
}

func TestBannerIsTheStraightFixture(t *testing.T) {
	S = Settings{}
	var buf bytes.Buffer
	PrintBanner(&buf, "1.2.3", "stable")
	want := strings.Join(Banner, "\n") + "\n\nWelcome to PaperToBillion CLI\nVersion 1.2.3 • stable\n"
	if buf.String() != want {
		t.Fatalf("banner differs:\n%q\nwant\n%q", buf.String(), want)
	}
	for _, l := range Banner {
		if strings.ContainsAny(l, "/\\") {
			t.Fatal("banner must stay upright")
		}
	}
}

func TestGroupInt(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1.000", -1234567: "-1.234.567"} {
		if got := Group(in); got != want {
			t.Fatalf("%d → %s, want %s", in, got, want)
		}
	}
}
