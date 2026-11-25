package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ealfarozi/ptb-cli/internal/api"
	"github.com/ealfarozi/ptb-cli/internal/ui"
)

// flagSpec maps a CLI flag to a query parameter.
type flagSpec struct {
	Name  string // CLI flag
	Query string // query parameter ("" = same as Name)
	Usage string
	Bool  bool
	Fixed string // always sent with this value
}

// dataSpec is one data command. Paths and parameters mirror the PTB MCP
// server (mcp/src/client.ts) so CLI and MCP read exactly the same data.
type dataSpec struct {
	Use     string   // e.g. "orderflow <SYMBOL>"
	Parent  string   // "" = top level, else a group command name
	Short   string
	Long    string
	Example string
	Tool    string   // MCP tool name (for `ptb tools describe`)
	Path    func(args []string, q url.Values) string
	NArgs   int      // required positional args
	OptArgs int      // optional positional args
	Flags   []flagSpec
	Paid    string   // quote tool id when the call may charge tokens
}

func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

var dateFlag = flagSpec{Name: "date", Usage: "tanggal YYYY-MM-DD (kosong = hari ini/terbaru)"}
var fromFlag = flagSpec{Name: "from", Usage: "tanggal awal YYYY-MM-DD"}
var toFlag = flagSpec{Name: "to", Usage: "tanggal akhir YYYY-MM-DD"}
var limitFlag = flagSpec{Name: "limit", Usage: "jumlah baris"}

const paidNote = "\n\nBERBAYAR untuk sesi hari ini: 1 token per 1.000 transaksi yang belum dibayar, minimal 1 token\n(sama dengan Play di web). Sesi yang sudah selesai (--date) gratis. CLI menampilkan perkiraan\nbiaya dan meminta konfirmasi; pakai --yes untuk langsung lanjut dan --max-tokens sebagai batas."

var specs = []dataSpec{
	// Market (free for live Prime members)
	{Use: "most-active", Short: "Saham teraktif", Tool: "ptb_get_most_active", Flags: []flagSpec{dateFlag},
		Path: func(a []string, q url.Values) string { return "/market/most-active" }},
	{Use: "bubbles", Short: "Market Bubbles: seluruh saham dalam satu peta", Tool: "ptb_get_bubbles",
		Flags: []flagSpec{{Name: "window", Usage: "jendela waktu, mis. 1D"}},
		Path:  func(a []string, q url.Values) string { return "/market/bubbles" }},
	{Use: "net-flow", Short: "Net Flow: HAKA/HAKI dan Big Trade per saham", Tool: "ptb_get_net_flow",
		Flags: []flagSpec{{Name: "sort", Usage: "net, net_sell, haka, haki, big_buy, big_sell, value"}, limitFlag, {Name: "symbols", Usage: "batasi ke kode saham, mis. BBCA,TLKM"}},
		Path:  func(a []string, q url.Values) string { return "/market/net-flow" }},
	{Use: "index-ptb", Short: "Index PTB aktif", Tool: "ptb_get_index_ptb",
		Path: func(a []string, q url.Values) string { return "/index-ptb/active" }},
	{Use: "index [CODE]", Short: "Indeks (IHSG, LQ45, …); tanpa kode = daftar indeks", Tool: "ptb_get_index", OptArgs: 1,
		Flags: []flagSpec{dateFlag, fromFlag, toFlag},
		Path: func(a []string, q url.Values) string {
			if len(a) == 0 {
				q.Del("from")
				q.Del("to")
				return "/indices"
			}
			return "/indices/" + url.PathEscape(upper(a[0]))
		}},
	{Use: "index-flow [CODE]", Short: "Aliran dana anggota indeks per menit (IHSG)", Tool: "ptb_get_index_flow", OptArgs: 1,
		Flags: []flagSpec{dateFlag, {Name: "step", Usage: "lebar bucket detik (60–3600)"}},
		Path: func(a []string, q url.Values) string {
			code := "IHSG"
			if len(a) > 0 {
				code = upper(a[0])
			}
			return "/indices/" + url.PathEscape(code) + "/flow"
		}},
	{Use: "premarket", Short: "Pre-opening: harga teoritis (IEP) dan auction", Tool: "ptb_get_premarket_auction",
		Flags: []flagSpec{dateFlag, {Name: "slot", Usage: "slot waktu, mis. 08:59"}, limitFlag},
		Path:  func(a []string, q url.Values) string { return "/premarket-auction" }},
	{Use: "foreign-flow", Short: "Foreign flow seluruh pasar", Tool: "ptb_get_foreign_flow", Flags: []flagSpec{fromFlag, toFlag},
		Path: func(a []string, q url.Values) string { return "/foreign-flow/snapshot" }},
	{Use: "today", Parent: "picks", Short: "Picks hari ini", Tool: "ptb_get_picks_today",
		Path: func(a []string, q url.Values) string { return "/picks/today" }},
	{Use: "history", Parent: "picks", Short: "Riwayat picks", Tool: "ptb_get_picks_history", Flags: []flagSpec{fromFlag, toFlag},
		Path: func(a []string, q url.Values) string { return "/picks/history" }},
	{Use: "stock <SYMBOL>", Short: "Profil saham", Tool: "ptb_get_stock_profile", NArgs: 1,
		Flags: []flagSpec{{Name: "full", Bool: true, Usage: "data lengkap (pemegang saham >1%, statistik, teknikal, foreign flow)"}},
		Path: func(a []string, q url.Values) string {
			full := q.Get("full") == "true"
			q.Del("full")
			if full {
				return "/stocks/" + url.PathEscape(upper(a[0])) + "/full"
			}
			return "/stocks/" + url.PathEscape(upper(a[0]))
		}},
	{Use: "corporate-action [SYMBOL]", Short: "Aksi korporasi; tanpa kode = feed terbaru seluruh pasar", Tool: "ptb_get_corporate_actions", OptArgs: 1,
		Flags: []flagSpec{limitFlag},
		Path: func(a []string, q url.Values) string {
			if len(a) > 0 {
				q.Del("limit")
				return "/corporate-action/symbol/" + url.PathEscape(upper(a[0]))
			}
			return "/corporate-action/feed"
		}},
	{Use: "scripless [TICKER]", Short: "Scripless: ringkasan, ranking, atau rincian satu saham", Tool: "ptb_get_scripless", OptArgs: 1,
		Flags: []flagSpec{{Name: "mode", Usage: "summary (default), top, ticker, history"}, {Name: "metric", Usage: "untuk top: lot atau frequency"},
			{Name: "period", Usage: "untuk top: 1D, 30D, 3M, 6M, YTD"}, fromFlag, toFlag, limitFlag},
		Path: func(a []string, q url.Values) string {
			mode := q.Get("mode")
			q.Del("mode")
			if mode == "" && len(a) > 0 {
				mode = "ticker"
			}
			t := ""
			if len(a) > 0 {
				t = url.PathEscape(upper(a[0]))
			}
			switch mode {
			case "top":
				return "/scripless/top"
			case "ticker":
				return "/scripless/ticker/" + t
			case "history":
				return "/scripless/ticker/" + t + "/history"
			}
			return "/scripless/summary"
		}},
	{Use: "watchlist", Short: "Watchlist kamu (bintang di web)", Tool: "ptb_get_watchlist",
		Path: func(a []string, q url.Values) string { return "/me/watchlist" }},
	{Use: "extreme-results", Short: "Kalender extreme results", Tool: "ptb_get_extreme_results",
		Flags: []flagSpec{{Name: "year", Usage: "tahun"}, {Name: "month", Usage: "bulan 1–12"}},
		Path:  func(a []string, q url.Values) string { return "/extreme-results/calendar" }},

	// Broker (free for live Prime members)
	{Use: "screener", Short: "Broker Screener (shark screener)", Tool: "get_shark_screener",
		Example: "  ptb screener --from 2026-11-01 --to 2026-11-24 --broker YP",
		Flags: []flagSpec{{Name: "from", Query: "startDate", Usage: "tanggal awal YYYY-MM-DD (wajib)"}, {Name: "to", Query: "endDate", Usage: "tanggal akhir YYYY-MM-DD (wajib)"},
			{Name: "broker", Query: "brokerCode", Usage: "filter kode broker"}, {Name: "extreme", Query: "isUsingExtremeList", Bool: true, Usage: "hanya saham extreme list"},
			{Name: "page", Usage: "halaman"}, limitFlag},
		Path: func(a []string, q url.Values) string { return "/broker-inventory/results" }},
	{Use: "summary <SYMBOL>", Parent: "broker", Short: "Broker summary satu saham", Tool: "get_broker_summary", NArgs: 1,
		Flags: []flagSpec{fromFlag, toFlag},
		Path:  func(a []string, q url.Values) string { q.Set("symbol", upper(a[0])); return "/broker-inventory/summary" }},
	{Use: "net-position <SYMBOL>", Parent: "broker", Short: "Posisi bersih broker per hari", Tool: "get_broker_net_position", NArgs: 1,
		Flags: []flagSpec{{Name: "from", Query: "startDate", Usage: "tanggal awal"}, {Name: "to", Query: "endDate", Usage: "tanggal akhir"}},
		Path:  func(a []string, q url.Values) string { q.Set("symbol", upper(a[0])); return "/broker-inventory/timeseries" }},
	{Use: "inventory <SYMBOLS>", Parent: "broker", Short: "Inventory broker (kode dipisah koma)", Tool: "get_inventory_chart", NArgs: 1,
		Flags: []flagSpec{fromFlag, toFlag, {Name: "brokers", Usage: "kode broker dipisah koma"}},
		Path:  func(a []string, q url.Values) string { q.Set("symbols", upper(a[0])); return "/broker-inventory/chart" }},
	{Use: "price <SYMBOL>", Parent: "broker", Short: "Harga harian untuk analisa broker", Tool: "get_price_chart", NArgs: 1,
		Flags: []flagSpec{fromFlag, toFlag},
		Path:  func(a []string, q url.Values) string { q.Set("symbol", upper(a[0])); return "/broker-inventory/price-chart" }},
	{Use: "foreign-list", Parent: "broker", Short: "Daftar broker asing", Tool: "get_foreign_brokers",
		Path: func(a []string, q url.Values) string { return "/config/foreign-brokers" }},
	{Use: "activity <BROKER>", Parent: "broker", Short: "Saham yang paling dibeli/dijual satu broker", Tool: "ptb_get_broker_activity", NArgs: 1,
		Flags: []flagSpec{fromFlag, toFlag, limitFlag},
		Path:  func(a []string, q url.Values) string { q.Set("broker", upper(a[0])); return "/broker-activity" }},
	{Use: "stalker <BROKER>", Parent: "broker", Short: "Broker Stalker: saham yang dikumpulkan satu broker", Tool: "get_broker_stalker", NArgs: 1,
		Flags: []flagSpec{fromFlag, toFlag, {Name: "limit", Query: "topN", Usage: "jumlah saham teratas"}, {Name: "net", Query: "useNet", Fixed: "true"}},
		Path:  func(a []string, q url.Values) string { q.Set("brokers", upper(a[0])); return "/broker-stalker/data" }},

	// Paid live tools
	{Use: "orderflow <SYMBOL>", Short: "Order Flow: delta, CVD, dan bar terakhir", Tool: "ptb_get_orderflow", NArgs: 1, Paid: "orderflow",
		Long: "Ringkasan Order Flow satu sesi: delta, CVD, VWAP, POC per bar." + paidNote, Example: "  ptb orderflow BBCA --tf 5m\n  ptb orderflow BBCA --date 2026-11-24   # sesi selesai, gratis",
		Flags: []flagSpec{dateFlag, {Name: "tf", Usage: "timeframe 1m, 3m, 5m, 15m, 30m, 1H (default 5m)"}, {Name: "bars", Usage: "jumlah bar terakhir (maks 120)"}},
		Path:  live("orderflow")},
	{Use: "big-trades <SYMBOL>", Short: "Big Trade: order terbesar", Tool: "ptb_get_big_trades", NArgs: 1, Paid: "big-trades", Long: "Order terbesar satu sesi." + paidNote,
		Flags: []flagSpec{dateFlag, {Name: "min-value", Query: "minValue", Usage: "nilai minimum (Rp)"}, limitFlag}, Path: live("big-trades")},
	{Use: "book-events <SYMBOL>", Short: "Wall yang dicabut/diserap di order book (hari ini)", Tool: "ptb_get_book_events", NArgs: 1, Paid: "book-events", Long: "Book Events hanya untuk sesi hari ini." + paidNote,
		Flags: []flagSpec{{Name: "percentile", Usage: "80, 90, 95, 99"}, {Name: "min-drop", Query: "minDrop", Usage: "30, 50, 70"}}, Path: live("book-events")},
	{Use: "auction-hub <SYMBOL>", Short: "Auction Hub: pre-opening dan pre-closing", Tool: "ptb_get_auction_hub", NArgs: 1, Paid: "auction-hub", Long: "Lelang pembukaan dan penutupan." + paidNote,
		Flags: []flagSpec{dateFlag}, Path: live("auction-hub")},
	{Use: "volume-profile <SYMBOL>", Short: "Volume Profile: POC, value area, HVN/LVN", Tool: "ptb_get_volume_profile", NArgs: 1, Paid: "volume-profile", Long: "Volume pada tiap harga." + paidNote,
		Flags: []flagSpec{dateFlag, {Name: "from-time", Query: "fromTime", Usage: "jam mulai HH:MM WIB"}, {Name: "to-time", Query: "toTime", Usage: "jam akhir HH:MM WIB"}}, Path: live("volume-profile")},
	{Use: "done-trades <SYMBOL>", Short: "Done trade (running trade) dengan broker", Tool: "ptb_get_done_trades", NArgs: 1, Paid: "done-trades", Long: "Transaksi terbaru beserta broker." + paidNote,
		Flags: []flagSpec{dateFlag, {Name: "broker", Usage: "kode broker"}, {Name: "side", Usage: "buy atau sell"}, {Name: "from-time", Query: "fromTime", Usage: "HH:MM"},
			{Name: "to-time", Query: "toTime", Usage: "HH:MM"}, {Name: "min-value", Query: "minValue", Usage: "nilai minimum (Rp)"}, limitFlag, {Name: "offset", Usage: "lewati sekian transaksi"}},
		Path: live("done-trades")},
	{Use: "heatmap <SYMBOL>", Short: "Heatmap order book per menit", Tool: "ptb_get_heatmap", NArgs: 1, Paid: "heatmap",
		Long:  "Order book beberapa menit terakhir.\n\nBERBAYAR untuk hari ini: 1 token per menit per saham; menit yang sudah dibayar tidak ditagih lagi.\nHari yang sudah selesai (--date) gratis. Pakai --yes untuk langsung lanjut.",
		Flags: []flagSpec{dateFlag, {Name: "minutes", Usage: "menit terakhir (maks 120)"}, {Name: "depth", Usage: "level bid/ask (maks 10)"}}, Path: live("heatmap")},
}

func live(kind string) func([]string, url.Values) string {
	return func(a []string, q url.Values) string { return "/live/" + kind + "/" + url.PathEscape(upper(a[0])) }
}

func wibToday() string {
	loc := time.FixedZone("WIB", 7*3600)
	return time.Now().In(loc).Format("2006-01-02")
}

func buildDataCmd(s dataSpec) *cobra.Command {
	values := map[string]*string{}
	bools := map[string]*bool{}
	c := &cobra.Command{
		Use: s.Use, Short: s.Short, Long: s.Long, Example: s.Example,
		Args: cobra.RangeArgs(s.NArgs, s.NArgs+s.OptArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			for _, f := range s.Flags {
				name := f.Query
				if name == "" {
					name = f.Name
				}
				switch {
				case f.Fixed != "":
					q.Set(name, f.Fixed)
				case f.Bool:
					if *bools[f.Name] {
						q.Set(name, "true")
					}
				case *values[f.Name] != "":
					q.Set(name, *values[f.Name])
				}
			}
			path := s.Path(args, q)
			return runData(cmd.Context(), s, args, path, q)
		},
	}
	if s.Long == "" {
		c.Long = s.Short + "."
	}
	for _, f := range s.Flags {
		if f.Fixed != "" {
			continue
		}
		if f.Bool {
			bools[f.Name] = c.Flags().Bool(f.Name, false, f.Usage)
		} else {
			values[f.Name] = c.Flags().String(f.Name, "", f.Usage)
		}
	}
	return c
}

func runData(ctx context.Context, s dataSpec, args []string, path string, q url.Values) error {
	c, _, err := client(ctx)
	if err != nil {
		return err
	}
	opts := api.Options{}
	date := q.Get("date")
	if s.Paid != "" && (date == "" || date == wibToday() || s.Paid == "book-events") {
		if opts, err = confirmPaid(ctx, c, s, upper(args[0]), date); err != nil {
			return err
		}
	}
	res, err := c.Get(ctx, path, q, opts)
	if err != nil {
		return err
	}
	if err := ui.Render(os.Stdout, res.Data, format()); err != nil {
		return err
	}
	if s.Paid != "" {
		reportBilling(res)
	}
	return nil
}

// confirmPaid shows the server quote and asks before a paid call. --yes skips
// the question; --max-tokens is sent to the server as a hard ceiling. Without
// --max-tokens the exact quote becomes the ceiling, so a confirmed call can
// never cost more than what was shown.
func confirmPaid(ctx context.Context, c *api.Client, s dataSpec, symbol, date string) (api.Options, error) {
	opts := api.Options{IdempotencyKey: api.NewIdempotencyKey(), MaxTokens: flagMaxTokens}
	q, err := c.Quote(ctx, s.Paid, symbol, date)
	if err != nil {
		return opts, err
	}
	if q.Free || (q.EstimatedTokens == 0 && q.Exact) {
		if q.Free {
			ui.Notef("%s", ui.Dim("Akun ini tidak dikenai token."))
		} else {
			ui.Notef("%s", ui.Dim("Sudah dibayar sebelumnya: 0 token."))
		}
		return opts, nil
	}
	est := fmt.Sprintf("%d token", q.EstimatedTokens)
	switch {
	case s.Paid == "heatmap":
		est = "maksimal " + est + " (menit yang sedang berjalan)"
	case !q.Exact:
		est = "minimal " + est + " (jumlah transaksi baru pasti saat dibuka)"
	}
	bal := ""
	if q.Balance != nil {
		bal = fmt.Sprintf(" · saldo %s token", ui.Group(*q.Balance))
	}
	ui.Notef("%s %s %s", ui.Bold(s.Tool), symbol, ui.Dim(q.Date))
	ui.Notef("Perkiraan biaya: %s%s", ui.Warn(est), bal)
	if flagMaxTokens > 0 {
		ui.Notef("Batas (--max-tokens): %d token, ditegakkan server", flagMaxTokens)
		if q.EstimatedTokens > flagMaxTokens {
			return opts, exitWith(api.ExitTokens, &api.Error{Code: "BUDGET_EXCEEDED", Message: fmt.Sprintf("perkiraan %d token melebihi --max-tokens %d", q.EstimatedTokens, flagMaxTokens)})
		}
	} else if q.Exact {
		opts.MaxTokens = q.EstimatedTokens
	}
	if q.Balance != nil && *q.Balance < q.EstimatedTokens {
		return opts, exitWith(api.ExitTokens, &api.Error{Code: "INSUFFICIENT_TOKENS", Message: fmt.Sprintf("saldo %d token, butuh %d. Top up di papertobillion.com/account/tokens/top-up", *q.Balance, q.EstimatedTokens)})
	}
	if flagYes {
		return opts, nil
	}
	if !ui.IsTTY(os.Stdin) {
		return opts, exitWith(api.ExitTokens, fmt.Errorf("perintah ini memotong token dan butuh konfirmasi: tambahkan --yes (opsional --max-tokens N) untuk menjalankan tanpa tanya"))
	}
	fmt.Fprint(os.Stderr, "Lanjutkan? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "ya", "yes":
		return opts, nil
	}
	return opts, exitWith(api.ExitCancelled, fmt.Errorf("dibatalkan, tidak ada token yang dipotong"))
}

func reportBilling(res *api.Response) {
	b := api.BillingOf(res.Data)
	if b == nil || flagQuiet {
		return
	}
	msg := fmt.Sprintf("Terpotong: %d token", b.ChargedTokens)
	if b.Free {
		msg = "Gratis untuk akun ini"
	}
	if b.BalanceAfter != nil {
		msg += fmt.Sprintf(" · sisa %s token", ui.Group(*b.BalanceAfter))
	}
	if b.Refunded > 0 {
		msg += fmt.Sprintf(" · dikembalikan %d token", b.Refunded)
	}
	if res.Replayed {
		msg += " · jawaban ulang tanpa potong lagi"
	}
	ui.Notef("%s", ui.Dim(msg))
}

func init() {
	groups := map[string]*cobra.Command{
		"broker": {Use: "broker", Short: "Data broker: summary, posisi, inventory, activity, stalker"},
		"picks":  {Use: "picks", Short: "Picks PTB"},
	}
	for _, g := range groups {
		rootCmd.AddCommand(g)
	}
	for _, s := range specs {
		c := buildDataCmd(s)
		if s.Parent != "" {
			groups[s.Parent].AddCommand(c)
		} else {
			rootCmd.AddCommand(c)
		}
	}
	// Spec-familiar alias: `ptb broker-stalker YP` = `ptb broker stalker YP`.
	for _, s := range specs {
		if s.Tool == "get_broker_stalker" {
			alias := buildDataCmd(s)
			alias.Use = "broker-stalker <BROKER>"
			alias.Hidden = true
			rootCmd.AddCommand(alias)
		}
	}
}
