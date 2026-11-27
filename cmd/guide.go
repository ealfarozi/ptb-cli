package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ealfarozi/ptb-cli/internal/api"
	"github.com/ealfarozi/ptb-cli/internal/config"
	"github.com/ealfarozi/ptb-cli/internal/ui"
)

// Same rules as the MCP tool ptb_get_guide: Help Center articles from the
// web, filtered to what the account may open.
var internalGuides = map[string]bool{"journal": true, "formula": true}

type guide struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Category string   `json:"category"`
	Summary  string   `json:"summary"`
	Tiers    []string `json:"tiers"`
	URL      string   `json:"url"`
	Text     string   `json:"text,omitempty"`
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

var guideCmd = &cobra.Command{
	Use:     "guide [FITUR]",
	Short:   "Panduan fitur PTB (cara pakai, istilah, biaya token)",
	Long:    "Tanpa argumen: daftar panduan yang bisa dibuka akun ini. Dengan FITUR (mis. net-flow, order-flow): isi panduan lengkap.",
	Example: "  ptb guide\n  ptb guide net-flow",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := client(cmd.Context())
		if err != nil {
			return err
		}
		st, err := c.Get(cmd.Context(), "/me/status", nil, api.Options{})
		if err != nil {
			return err
		}
		var status struct {
			Tier     string `json:"tier"`
			Internal bool   `json:"internal"`
		}
		_ = json.Unmarshal(st.Data, &status)

		web := os.Getenv("PTB_WEB_URL")
		if web == "" {
			web = config.DefaultWeb
		}
		hc := &http.Client{Timeout: 15 * time.Second}
		res, err := hc.Get(web + "/api/help/guides")
		if err != nil {
			return exitWith(api.ExitNetwork, fmt.Errorf("panduan belum bisa dimuat: %w", err))
		}
		defer res.Body.Close()
		var body struct {
			Data []guide `json:"data"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil || res.StatusCode != 200 {
			return exitWith(api.ExitNetwork, fmt.Errorf("panduan belum bisa dimuat (HTTP %d)", res.StatusCode))
		}
		var visible []guide
		for _, g := range body.Data {
			switch {
			case status.Tier == "admin":
			case internalGuides[g.ID] && !status.Internal:
				continue
			case status.Tier == "sinyal" && !has(g.Tiers, "full"):
				continue
			case status.Tier != "sinyal" && !has(g.Tiers, "lite"):
				continue
			}
			visible = append(visible, g)
		}
		if len(args) == 0 {
			rows := make([]map[string]string, 0, len(visible))
			for _, g := range visible {
				rows = append(rows, map[string]string{"1 id": g.ID, "2 judul": g.Title, "3 kategori": g.Category})
			}
			if format() != ui.Table {
				for i := range visible {
					visible[i].Text = ""
				}
				b, _ := json.Marshal(visible)
				return ui.Render(os.Stdout, b, format())
			}
			b, _ := json.Marshal(rows)
			ui.Notef("%s", ui.Dim("Buka satu panduan: ptb guide <id>"))
			return ui.Render(os.Stdout, b, ui.Table)
		}
		want := strings.ToLower(strings.TrimSpace(args[0]))
		for _, g := range visible {
			if g.ID == want {
				if format() != ui.Table {
					b, _ := json.Marshal(g)
					return ui.Render(os.Stdout, b, format())
				}
				fmt.Println(g.Text)
				fmt.Println()
				fmt.Println(ui.Dim(g.URL))
				return nil
			}
		}
		return exitWith(api.ExitUsage, fmt.Errorf("panduan %q tidak ada atau tidak terbuka untuk akun ini; lihat `ptb guide`", want))
	},
}

func init() { rootCmd.AddCommand(guideCmd) }
