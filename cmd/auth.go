package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/ealfarozi/ptb-cli/internal/api"
	"github.com/ealfarozi/ptb-cli/internal/auth"
	"github.com/ealfarozi/ptb-cli/internal/ui"
	"github.com/ealfarozi/ptb-cli/internal/version"
)

func openBrowser(u string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", u)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		c = exec.Command("xdg-open", u)
	}
	return c.Start()
}

var flagNoBrowser bool

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Masuk lewat halaman login PTB di browser",
	Long: `Membuka halaman login PTB di browser. Login seperti biasa (Discord, Google, atau passkey),
lalu tekan Setujui. CLI menyimpan sesi di keychain sistem dan tampil di menu
Integrasi → Aplikasi terhubung, tempat kamu bisa mencabutnya kapan saja.

Untuk server/CI tanpa browser: buat API key di Integrasi → Keamanan lalu set PTB_API_KEY.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
		defer cancel()
		prof := cfg.Profile(flagProfile)
		if ui.S.Banner {
			ui.PrintBanner(os.Stderr, version.Version, version.Channel)
			fmt.Fprintln(os.Stderr)
		}
		clientID := prof.ClientID
		if clientID == "" {
			id, err := auth.Register(ctx, prof.Issuer)
			if err != nil {
				return exitWith(api.ExitNetwork, err)
			}
			clientID = id
			cfg.SetClientID(flagProfile, id)
			if err := cfg.Save(); err != nil {
				ui.Errf("%s", ui.Caution("config tidak tersimpan: "+err.Error()))
			}
		}
		sess, err := auth.Login(ctx, prof.Issuer, clientID, func(u string) {
			ui.Notef("Membuka halaman login aman di browser…")
			if flagNoBrowser || openBrowser(u) != nil {
				ui.Errf("Buka URL ini di browser:\n\n  %s\n", u)
			}
			ui.Notef("%s", ui.Dim("Menunggu persetujuan di papertobillion.com (Ctrl+C untuk batal)"))
		})
		if err != nil && prof.ClientID != "" && ctx.Err() == nil {
			// The stored client may be gone on the server: forget it so the
			// next `ptb login` registers a fresh one.
			cfg.SetClientID(flagProfile, "")
			_ = cfg.Save()
			err = errors.New(err.Error() + " (coba `ptb login` sekali lagi)")
		}
		if err != nil {
			return exitWith(api.ExitAuth, err)
		}
		where, err := auth.Save(flagProfile, sess)
		if err != nil {
			return err
		}
		ui.Notef("%s", ui.OK("Berhasil masuk. Sesi disimpan di "+where+"."))
		c := api.New(prof.API, sess.AccessToken, flagTimeout)
		if res, err := c.Get(ctx, "/me/status", nil, api.Options{}); err == nil {
			printStatusLine(res.Data)
		}
		return nil
	},
}

func printStatusLine(data json.RawMessage) {
	var s map[string]interface{}
	if json.Unmarshal(data, &s) != nil {
		return
	}
	name, _ := s["username"].(string)
	if name == "" {
		name, _ = s["name"].(string)
	}
	tier, _ := s["tier"].(string)
	ui.Notef("Akun: %s  ·  akses: %s", ui.Bold(name), tier)
}

var flagAllDevices bool

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Keluar: hapus sesi di komputer ini dan cabut di server",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		prof := cfg.Profile(flagProfile)
		s, err := auth.Load(flagProfile)
		if err != nil {
			ui.Notef("Tidak ada sesi tersimpan untuk profil %q.", flagProfile)
		} else {
			if s.RefreshToken != "" {
				issuer := s.Issuer
				if issuer == "" {
					issuer = prof.Issuer
				}
				if rerr := auth.Revoke(cmd.Context(), issuer, s.ClientID, s.RefreshToken); rerr != nil {
					ui.Errf("%s", ui.Caution("pencabutan di server gagal ("+rerr.Error()+"); sesi lokal tetap dihapus. Cabut manual di Integrasi → Aplikasi terhubung."))
				}
			}
			auth.Delete(flagProfile)
			ui.Notef("%s", ui.OK("Keluar dari profil "+flagProfile+"."))
		}
		if flagAllDevices {
			ui.Notef("Untuk mencabut semua perangkat dan aplikasi, buka %s/ai-mcp?tab=apps", "https://papertobillion.com")
		}
		if os.Getenv("PTB_API_KEY") != "" {
			ui.Notef("%s", ui.Caution("PTB_API_KEY masih di-set di environment; hapus atau cabut key-nya di Integrasi → Keamanan."))
		}
		return nil
	},
}

var whoamiCmd = &cobra.Command{
	Use:     "whoami",
	Aliases: []string{"status"},
	Short:   "Akun yang sedang dipakai dan sumber login",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, src, err := client(cmd.Context())
		if err != nil {
			return err
		}
		res, err := c.Get(cmd.Context(), "/me/status", nil, api.Options{})
		if err != nil {
			return err
		}
		if format() == ui.Table {
			fmt.Printf("%s  %s\n", ui.Dim("login  "), string(src))
			fmt.Printf("%s  %s\n", ui.Dim("profil "), flagProfile)
			if s, err := auth.Load(flagProfile); err == nil && src == auth.SourceOAuth {
				fmt.Printf("%s  %s\n", ui.Dim("berlaku"), s.Expiry.Local().Format("2006-01-02 15:04"))
			}
			fmt.Println()
		}
		return ui.Render(os.Stdout, res.Data, format())
	},
}

var authCmd = &cobra.Command{Use: "auth", Short: "Login, logout, dan status sesi"}

func init() {
	loginCmd.Flags().BoolVar(&flagNoBrowser, "no-browser", false, "jangan buka browser otomatis; tampilkan URL saja")
	logoutCmd.Flags().BoolVar(&flagAllDevices, "all-devices", false, "tampilkan cara mencabut semua perangkat")
	authStatus := *whoamiCmd
	authStatus.Use, authStatus.Aliases = "status", nil
	authCmd.AddCommand(&authStatus)
	rootCmd.AddCommand(loginCmd, logoutCmd, whoamiCmd, authCmd)
}

// home is `ptb` without arguments on a terminal.
func home(cmd *cobra.Command) error {
	if ui.S.Banner {
		ui.PrintBanner(os.Stdout, version.Version, version.Channel)
		fmt.Println()
	}
	if _, err := auth.Load(flagProfile); err != nil && os.Getenv("PTB_API_KEY") == "" {
		fmt.Println("Belum login.  " + ui.Bold("ptb login") + ui.Dim("  — masuk lewat browser"))
	} else {
		fmt.Println(ui.Good("✓") + " Sudah login (profil " + flagProfile + ")")
	}
	fmt.Println()
	rows := [][2]string{
		{"ptb account status", "membership dan akses"},
		{"ptb tokens balance", "saldo token"},
		{"ptb tools list", "semua tool dan tarifnya"},
		{"ptb broker summary BBCA", "broker summary (gratis, Prime)"},
		{"ptb orderflow BBCA", "Order Flow (token untuk sesi hari ini)"},
		{"ptb help", "semua perintah"},
	}
	for _, r := range rows {
		fmt.Printf("  %-26s %s\n", ui.Info(r[0]), ui.Dim(r[1]))
	}
	return nil
}
