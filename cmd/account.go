package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ealfarozi/ptb-cli/internal/api"
	"github.com/ealfarozi/ptb-cli/internal/ui"
)

func simple(use, short, path string) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := client(cmd.Context())
			if err != nil {
				return err
			}
			res, err := c.Get(cmd.Context(), path, nil, api.Options{})
			if err != nil {
				return err
			}
			return ui.Render(os.Stdout, res.Data, format())
		},
	}
}

var (
	accountCmd = &cobra.Command{Use: "account", Short: "Akun dan membership"}
	tokensCmd  = &cobra.Command{Use: "tokens", Short: "Saldo token"}
	billingCmd = &cobra.Command{Use: "billing", Short: "Riwayat pemakaian dan perkiraan biaya"}
	toolsCmd   = &cobra.Command{Use: "tools", Short: "Daftar tool PTB, akses, dan tarif"}
)

var histDays, histLimit int

var billingHistoryCmd = &cobra.Command{
	Use: "history", Short: "Riwayat pemakaian token (sama dengan Integrasi → Pemakaian token)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := client(cmd.Context())
		if err != nil {
			return err
		}
		q := url.Values{}
		if histDays > 0 {
			q.Set("days", strconv.Itoa(histDays))
		}
		if histLimit > 0 {
			q.Set("limit", strconv.Itoa(histLimit))
		}
		res, err := c.Get(cmd.Context(), "/me/usage", q, api.Options{})
		if err != nil {
			return err
		}
		return ui.Render(os.Stdout, res.Data, format())
	},
}

var estDate string

var billingEstimateCmd = &cobra.Command{
	Use:   "estimate <TOOL> <SYMBOL>",
	Short: "Perkiraan biaya tool berbayar tanpa memotong token",
	Long: `Menampilkan perkiraan biaya dari server tanpa memotong token.
TOOL: orderflow, big-trades, book-events, auction-hub, volume-profile, done-trades, heatmap.`,
	Example: "  ptb billing estimate orderflow BBCA",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := client(cmd.Context())
		if err != nil {
			return err
		}
		q := url.Values{"tool": {strings.ToLower(args[0])}, "symbol": {upper(args[1])}}
		if estDate != "" {
			q.Set("date", estDate)
		}
		res, err := c.Get(cmd.Context(), "/quote", q, api.Options{})
		if err != nil {
			return err
		}
		return ui.Render(os.Stdout, res.Data, format())
	},
}

type catalogTool struct {
	Name    string `json:"name"`
	Group   string `json:"group"`
	Title   string `json:"title"`
	Access  string `json:"access"`
	Price   string `json:"price"`
	Detail  string `json:"detail"`
	Params  string `json:"params"`
	History string `json:"history"`
	Example string `json:"example"`
	Command string `json:"command,omitempty"`
}

var accessLabel = map[string]string{"sinyal": "Prime aktif", "token": "Punya token", "account": "Semua member"}

func commandFor(tool string) string {
	for _, s := range specs {
		if s.Tool == tool {
			use := strings.Fields(s.Use)[0]
			if s.Parent != "" {
				use = s.Parent + " " + use
			}
			return "ptb " + use
		}
	}
	switch tool {
	case "ptb_get_account_status":
		return "ptb account status"
	case "ptb_get_wallet_balance":
		return "ptb tokens balance"
	case "ptb_get_usage_history":
		return "ptb billing history"
	}
	return ""
}

func loadCatalog(cmd *cobra.Command) ([]catalogTool, error) {
	c, _, err := client(cmd.Context())
	if err != nil {
		return nil, err
	}
	res, err := c.Get(cmd.Context(), "/tools", nil, api.Options{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []catalogTool `json:"tools"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return nil, err
	}
	for i := range out.Tools {
		out.Tools[i].Command = commandFor(out.Tools[i].Name)
	}
	return out.Tools, nil
}

var toolsListCmd = &cobra.Command{
	Use: "list", Short: "Semua tool dengan akses dan tarifnya", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tools, err := loadCatalog(cmd)
		if err != nil {
			return err
		}
		if format() != ui.Table {
			b, _ := json.Marshal(tools)
			return ui.Render(os.Stdout, b, format())
		}
		rows := make([]map[string]string, 0, len(tools))
		for _, t := range tools {
			rows = append(rows, map[string]string{"1 perintah": t.Command, "2 judul": t.Title, "3 akses": accessLabel[t.Access], "4 tarif": t.Price})
		}
		b, _ := json.Marshal(rows)
		return ui.Render(os.Stdout, b, ui.Table)
	},
}

var toolsDescribeCmd = &cobra.Command{
	Use: "describe <TOOL|PERINTAH>", Short: "Detail satu tool: isi, parameter, tarif, contoh", Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		want := strings.Join(args, " ")
		tools, err := loadCatalog(cmd)
		if err != nil {
			return err
		}
		for _, t := range tools {
			if t.Name == want || t.Command == want || t.Command == "ptb "+want {
				b, _ := json.Marshal(t)
				return ui.Render(os.Stdout, b, format())
			}
		}
		return exitWith(api.ExitUsage, fmt.Errorf("tool %q tidak ada; lihat `ptb tools list`", want))
	},
}

func init() {
	accountCmd.AddCommand(simple("status", "Status akun, membership, dan akses tool", "/me/status"))
	tokensCmd.AddCommand(simple("balance", "Saldo token, token yang akan kedaluwarsa, dan link top up", "/me/wallet"))
	billingHistoryCmd.Flags().IntVar(&histDays, "days", 0, "rentang hari ke belakang (1–90, default 7)")
	billingHistoryCmd.Flags().IntVar(&histLimit, "limit", 0, "jumlah baris (1–100, default 30)")
	billingEstimateCmd.Flags().StringVar(&estDate, "date", "", "tanggal YYYY-MM-DD (kosong = hari ini)")
	billingCmd.AddCommand(billingHistoryCmd, billingEstimateCmd)
	toolsCmd.AddCommand(toolsListCmd, toolsDescribeCmd)
	rootCmd.AddCommand(accountCmd, tokensCmd, billingCmd, toolsCmd)
}
