package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ealfarozi/ptb-cli/internal/api"
	"github.com/ealfarozi/ptb-cli/internal/auth"
	"github.com/ealfarozi/ptb-cli/internal/config"
	"github.com/ealfarozi/ptb-cli/internal/ui"
	"github.com/ealfarozi/ptb-cli/internal/version"
)

const repoSlug = "ealfarozi/ptb-cli"

var versionCmd = &cobra.Command{
	Use: "version", Short: "Versi, channel, dan commit build ini", Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if format() == ui.JSON {
			b, _ := json.Marshal(map[string]string{"version": version.Version, "channel": version.Channel, "commit": version.Commit, "date": version.Date, "os": runtime.GOOS, "arch": runtime.GOARCH})
			fmt.Println(string(b))
			return
		}
		fmt.Println(version.String())
	},
}

var configCmd = &cobra.Command{Use: "config", Short: "Pengaturan tampilan dan profil"}

func init() {
	configCmd.AddCommand(
		&cobra.Command{Use: "get <KEY>", Short: "Nilai satu pengaturan", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				v, err := cfg.Get(flagProfile, args[0])
				if err != nil {
					return exitWith(api.ExitUsage, err)
				}
				fmt.Println(v)
				return nil
			}},
		&cobra.Command{Use: "set <KEY> <VALUE>", Short: "Ubah pengaturan (" + strings.Join(config.Keys, ", ") + ")", Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := cfg.Set(flagProfile, args[0], args[1]); err != nil {
					return exitWith(api.ExitUsage, err)
				}
				return cfg.Save()
			}},
		&cobra.Command{Use: "list", Short: "Semua pengaturan", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				for _, kv := range cfg.List(flagProfile) {
					v := kv[1]
					if v == "" {
						v = ui.Dim("(bawaan)")
					}
					fmt.Printf("%-15s %s\n", kv[0], v)
				}
				return nil
			}},
		&cobra.Command{Use: "reset <KEY>", Short: "Kembalikan pengaturan ke bawaan", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := cfg.Set(flagProfile, args[0], ""); err != nil {
					return exitWith(api.ExitUsage, err)
				}
				return cfg.Save()
			}},
	)
	rootCmd.AddCommand(versionCmd, configCmd, doctorCmd, updateCmd, agentCmd)
	updateCmd.AddCommand(&cobra.Command{Use: "check", Short: "Cek versi terbaru tanpa memasang", Args: cobra.NoArgs, RunE: runUpdateCheck})
	agentCmd.AddCommand(&cobra.Command{Use: "instructions", Short: "Panduan singkat untuk agen AI yang memakai ptb", Args: cobra.NoArgs, RunE: runAgentInstructions})
}

var doctorCmd = &cobra.Command{
	Use: "doctor", Short: "Diagnosa lingkungan (tanpa menampilkan rahasia)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		prof := cfg.Profile(flagProfile)
		dir, _ := config.Dir()
		checks := []map[string]string{}
		add := func(name, status, detail string) {
			checks = append(checks, map[string]string{"check": name, "status": status, "detail": detail})
		}
		add("versi", "ok", version.String())
		add("config", "ok", filepath.Join(dir, "config.json"))
		add("api", "ok", prof.API)
		if os.Getenv("PTB_API_KEY") != "" {
			add("login", "ok", "PTB_API_KEY di-set")
		} else if s, err := auth.Load(flagProfile); err == nil {
			st := "ok"
			if time.Until(s.Expiry) < 0 {
				st = "info"
			}
			add("login", st, "sesi browser, akses berlaku s.d. "+s.Expiry.Local().Format("2006-01-02 15:04")+" (diperpanjang otomatis)")
		} else {
			add("login", "gagal", "belum login: ptb login")
		}
		hc := &http.Client{Timeout: 10 * time.Second}
		base := strings.TrimSuffix(prof.API, "/api/v2/mcp")
		if res, err := hc.Get(base + "/health"); err != nil {
			add("server", "gagal", err.Error())
		} else {
			res.Body.Close()
			add("server", map[bool]string{true: "ok", false: "gagal"}[res.StatusCode == 200], fmt.Sprintf("HTTP %d", res.StatusCode))
		}
		if c, _, err := client(cmd.Context()); err == nil {
			if _, err := c.Get(cmd.Context(), "/me/status", nil, api.Options{}); err != nil {
				add("akses", "gagal", err.Error())
			} else {
				add("akses", "ok", "token diterima server")
			}
		}
		add("terminal", "info", fmt.Sprintf("tty=%v color=%v", ui.IsTTY(os.Stdout), ui.S.Color))
		b, _ := json.Marshal(checks)
		return ui.Render(os.Stdout, b, format())
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Cara memperbarui ptb",
	Long:  "Menampilkan versi terbaru dan perintah pembaruan sesuai cara ptb dipasang (Homebrew, WinGet, atau unduhan manual).",
	Args:  cobra.NoArgs,
	RunE:  runUpdateCheck,
}

func latestRelease() (string, string, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repoSlug+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", "", fmt.Errorf("GitHub HTTP %d", res.StatusCode)
	}
	var r struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return "", "", err
	}
	return strings.TrimPrefix(r.Tag, "v"), r.URL, nil
}

func installMethod() string {
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	switch {
	case strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/homebrew/") || strings.Contains(exe, "linuxbrew"):
		return "brew upgrade ptb"
	case strings.Contains(strings.ToLower(exe), "winget"):
		return "winget upgrade ealfarozi.ptb"
	}
	return "unduh rilis terbaru dari https://github.com/" + repoSlug + "/releases/latest"
}

func runUpdateCheck(cmd *cobra.Command, args []string) error {
	latest, link, err := latestRelease()
	if err != nil {
		return exitWith(api.ExitNetwork, fmt.Errorf("tidak bisa cek versi terbaru: %w", err))
	}
	cur := strings.TrimPrefix(version.Version, "v")
	if format() == ui.JSON {
		b, _ := json.Marshal(map[string]interface{}{"current": cur, "latest": latest, "updateAvailable": cur != latest, "url": link, "command": installMethod()})
		fmt.Println(string(b))
		return nil
	}
	if cur == latest {
		fmt.Println(ui.OK("ptb " + cur + " sudah versi terbaru."))
		return nil
	}
	fmt.Printf("Versi terpasang %s, terbaru %s\n%s\nPerbarui: %s\n", cur, ui.Bold(latest), ui.Dim(link), ui.Info(installMethod()))
	return nil
}

const agentText = `PTB CLI untuk agen AI

- Selalu pakai --output json (atau ndjson/csv). stdout hanya berisi data; pesan ada di stderr.
- Periksa akses dulu: ptb account status --output json ; saldo: ptb tokens balance --output json
- Daftar tool + tarif: ptb tools list --output json ; detail: ptb tools describe <tool>
- Tool berbayar (orderflow, big-trades, book-events, auction-hub, volume-profile, done-trades, heatmap)
  memotong token untuk sesi hari ini. Sesi yang sudah selesai (--date YYYY-MM-DD) gratis.
- Sebelum memanggil tool berbayar, tanyakan dulu ke user atau pakai perkiraan:
  ptb billing estimate orderflow BBCA --output json
- Tanpa terminal interaktif, tool berbayar butuh --yes. Selalu sertakan --max-tokens N sebagai batas;
  server menolak (BUDGET_EXCEEDED, exit 5) bila perkiraan melebihi batas, sebelum token dipotong.
- Kode keluar: 0 ok, 2 argumen salah, 3 perlu login, 4 tidak punya akses, 5 token kurang/batas,
  6 pasar tutup/riwayat, 7 rate limit, 8 jaringan/server, 10 dibatalkan.
- Jangan pernah menampilkan atau menyimpan PTB_API_KEY.
`

func runAgentInstructions(cmd *cobra.Command, args []string) error {
	if format() == ui.JSON {
		b, _ := json.Marshal(map[string]string{"instructions": agentText})
		fmt.Println(string(b))
		return nil
	}
	fmt.Print(agentText)
	return nil
}

var agentCmd = &cobra.Command{Use: "agent", Short: "Bantuan untuk agen AI"}
