package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ealfarozi/ptb-cli/internal/api"
	"github.com/ealfarozi/ptb-cli/internal/ui"
)

// Journal: same routes as the MCP tools ptb_journal_read / ptb_journal_write
// (server limits them to admin and internal accounts for now).

var journalCmd = &cobra.Command{
	Use:   "journal",
	Short: "Jurnal trading PTB (sementara khusus admin dan akun internal)",
}

var jFrom, jTo, jAccounts, jStatus, jDim, jType, jWeek, jID, jDate, jData string
var jVoid bool

func jPath(resource string) (string, error) {
	need := func(v, what string) error {
		if v == "" {
			return fmt.Errorf("--%s wajib untuk %s", what, resource)
		}
		return nil
	}
	switch resource {
	case "accounts", "summary", "episodes", "analytics", "daily", "notes", "playbooks", "reviews":
		return "/journal/" + resource, nil
	case "events":
		return "/journal/events", nil
	case "event-audit":
		return "/journal/events/" + url.PathEscape(jID) + "/audit", need(jID, "id")
	case "episode-checks":
		return "/journal/episodes/" + url.PathEscape(jID) + "/checks", need(jID, "id")
	case "daily-day":
		return "/journal/daily/" + url.PathEscape(jDate), need(jDate, "date")
	case "reviews-weekly":
		return "/journal/reviews/weekly", nil
	case "formula-catalog":
		return "/journal/formula/catalog", nil
	case "scan-access":
		return "/journal/formula/scan-access", nil
	}
	return "", fmt.Errorf("resource tidak dikenal %q", resource)
}

var journalReadCmd = &cobra.Command{
	Use:   "read <RESOURCE>",
	Short: "Baca Jurnal",
	Long: `RESOURCE: accounts, events, event-audit (--id), summary, episodes, episode-checks (--id),
analytics, daily, daily-day (--date), notes, playbooks, reviews, reviews-weekly, formula-catalog, scan-access.`,
	Example: "  ptb journal read summary --from 2026-11-01 --to 2026-11-30\n  ptb journal read analytics --dim playbook",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := jPath(args[0])
		if err != nil {
			return exitWith(api.ExitUsage, err)
		}
		q := url.Values{}
		for k, v := range map[string]string{"from": jFrom, "to": jTo, "accounts": jAccounts, "status": jStatus, "dim": jDim, "type": jType, "week": jWeek} {
			if v != "" {
				q.Set(k, v)
			}
		}
		if jVoid {
			q.Set("void", "true")
		}
		c, _, err := client(cmd.Context())
		if err != nil {
			return err
		}
		res, err := c.Get(cmd.Context(), path, q, api.Options{})
		if err != nil {
			return err
		}
		return ui.Render(os.Stdout, res.Data, format())
	},
}

type jWrite struct {
	method string
	path   func(id, date string) string
	needID bool
}

var journalWrites = map[string]jWrite{
	"create-account":   {"POST", func(_, _ string) string { return "/journal/accounts" }, false},
	"update-account":   {"PUT", func(id, _ string) string { return "/journal/accounts/" + id }, true},
	"delete-account":   {"DELETE", func(id, _ string) string { return "/journal/accounts/" + id }, true},
	"create-event":     {"POST", func(_, _ string) string { return "/journal/events" }, false},
	"update-event":     {"PUT", func(id, _ string) string { return "/journal/events/" + id }, true},
	"void-event":       {"POST", func(id, _ string) string { return "/journal/events/" + id + "/void" }, true},
	"save-daily":       {"PUT", func(_, d string) string { return "/journal/daily/" + d }, false},
	"create-note":      {"POST", func(_, _ string) string { return "/journal/notes" }, false},
	"update-note":      {"PUT", func(id, _ string) string { return "/journal/notes/" + id }, true},
	"delete-note":      {"DELETE", func(id, _ string) string { return "/journal/notes/" + id }, true},
	"create-playbook":  {"POST", func(_, _ string) string { return "/journal/playbooks" }, false},
	"update-playbook":  {"PUT", func(id, _ string) string { return "/journal/playbooks/" + id }, true},
	"delete-playbook":  {"DELETE", func(id, _ string) string { return "/journal/playbooks/" + id }, true},
	"save-review":      {"PUT", func(_, _ string) string { return "/journal/reviews" }, false},
	"validate-formula": {"POST", func(_, _ string) string { return "/journal/formula/validate" }, false},
	"eval-formula":     {"POST", func(_, _ string) string { return "/journal/formula/eval" }, false},
	"scan-playbook":    {"POST", func(id, _ string) string { return "/journal/playbooks/" + id + "/scan" }, true},
}

var journalWriteCmd = &cobra.Command{
	Use:   "write <ACTION>",
	Short: "Ubah Jurnal (selalu minta konfirmasi; --yes untuk lewati)",
	Long: `ACTION: create-account, update-account, delete-account, create-event, update-event, void-event,
save-daily (--date), create-note, update-note, delete-note, create-playbook, update-playbook,
delete-playbook, save-review, validate-formula, eval-formula, scan-playbook.
Isi perubahan lewat --data '<JSON>' (atau --data @file.json). Tidak mengirim order ke broker.`,
	Example: `  ptb journal write create-note --data '{"title":"Evaluasi","body":"…"}'`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		w, ok := journalWrites[args[0]]
		if !ok {
			return exitWith(api.ExitUsage, fmt.Errorf("action tidak dikenal %q; lihat `ptb journal write --help`", args[0]))
		}
		if w.needID && jID == "" {
			return exitWith(api.ExitUsage, fmt.Errorf("--id wajib untuk %s", args[0]))
		}
		if args[0] == "save-daily" && jDate == "" {
			return exitWith(api.ExitUsage, fmt.Errorf("--date wajib untuk save-daily"))
		}
		var body []byte
		if jData != "" {
			raw := jData
			if strings.HasPrefix(raw, "@") {
				b, err := os.ReadFile(raw[1:])
				if err != nil {
					return exitWith(api.ExitUsage, err)
				}
				raw = string(b)
			}
			var v interface{}
			if err := json.Unmarshal([]byte(raw), &v); err != nil {
				return exitWith(api.ExitUsage, fmt.Errorf("--data bukan JSON valid: %w", err))
			}
			body = []byte(raw)
		} else if w.method != "DELETE" {
			body = []byte("{}")
		}
		path := w.path(url.PathEscape(jID), url.PathEscape(jDate))
		ui.Notef("%s %s %s", ui.Bold(w.method), path, ui.Dim(string(body)))
		if !flagYes {
			if !ui.IsTTY(os.Stdin) {
				return exitWith(api.ExitUsage, fmt.Errorf("perubahan Jurnal butuh konfirmasi: tambahkan --yes"))
			}
			fmt.Fprint(os.Stderr, "Simpan perubahan ini? [y/N] ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "ya" && a != "yes" {
				return exitWith(api.ExitCancelled, fmt.Errorf("dibatalkan, Jurnal tidak diubah"))
			}
		}
		c, _, err := client(cmd.Context())
		if err != nil {
			return err
		}
		res, err := c.Send(cmd.Context(), w.method, path, nil, body)
		if err != nil {
			return err
		}
		return ui.Render(os.Stdout, res.Data, format())
	},
}

func init() {
	f := journalReadCmd.Flags()
	f.StringVar(&jFrom, "from", "", "awal rentang YYYY-MM-DD")
	f.StringVar(&jTo, "to", "", "akhir rentang YYYY-MM-DD")
	f.StringVar(&jAccounts, "accounts", "", "ID akun dipisah koma")
	f.StringVar(&jStatus, "status", "", "episodes: open / closed")
	f.StringVar(&jDim, "dim", "", "analytics: symbol, playbook, tag, month")
	f.StringVar(&jType, "type", "", "reviews: jenis review")
	f.StringVar(&jWeek, "week", "", "reviews-weekly: mis. 2026-W48")
	f.StringVar(&jID, "id", "", "ID event/episode")
	f.StringVar(&jDate, "date", "", "YYYY-MM-DD untuk daily-day")
	f.BoolVar(&jVoid, "void", false, "events: ikut tampilkan event yang dibatalkan")
	wf := journalWriteCmd.Flags()
	wf.StringVar(&jID, "id", "", "ID akun/event/catatan/playbook")
	wf.StringVar(&jDate, "date", "", "YYYY-MM-DD untuk save-daily")
	wf.StringVar(&jData, "data", "", "isi JSON, atau @file.json")
	journalCmd.AddCommand(journalReadCmd, journalWriteCmd)
	rootCmd.AddCommand(journalCmd)
}
