// Package cmd defines the ptb commands.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/ealfarozi/ptb-cli/internal/api"
	"github.com/ealfarozi/ptb-cli/internal/auth"
	"github.com/ealfarozi/ptb-cli/internal/config"
	"github.com/ealfarozi/ptb-cli/internal/ui"
	"github.com/ealfarozi/ptb-cli/internal/version"
)

// Global flags.
var (
	flagProfile      string
	flagOutput       string
	flagNoColor      bool
	flagPlain        bool
	flagNoAnimations bool
	flagNoBanner     bool
	flagTimeout      time.Duration
	flagMaxTokens    int64
	flagYes          bool
	flagQuiet        bool
	flagVerbose      bool
)

// exitError carries an exit code to main.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func exitWith(code int, err error) error { return &exitError{code: code, err: err} }

var cfg *config.File

var rootCmd = &cobra.Command{
	Use:   "ptb",
	Short: "PaperToBillion CLI — data PTB dari terminal, skrip, dan agen AI",
	Long: `PTB CLI membaca data PTB (broker flow, foreign flow, picks, Order Flow, Heatmap, dan lainnya)
langsung dari terminal. Membership, saldo token, dan tarifnya sama dengan web dan menu Integrasi.

Mulai:  ptb login   lalu   ptb account status`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return setup(cmd)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if !ui.IsTTY(os.Stdout) {
			return cmd.Help()
		}
		return home(cmd)
	},
}

func setup(cmd *cobra.Command) error {
	var err error
	cfg, err = config.Load()
	if err != nil {
		ui.Errf("%s", ui.Caution("config tidak terbaca, memakai bawaan: "+err.Error()))
	}
	if flagOutput == "" {
		flagOutput = cfg.Output
	}
	if flagOutput == "" {
		flagOutput = "table"
	}
	if _, err := ui.ParseFormat(flagOutput); err != nil {
		return exitWith(api.ExitUsage, err)
	}
	tty := ui.IsTTY(os.Stdout)
	structured := flagOutput != "table"
	color := tty && !flagNoColor && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && !structured
	if cfg.UI.Color != nil && !*cfg.UI.Color {
		color = false
	}
	anim := tty && color && !flagNoAnimations && !flagPlain && os.Getenv("CI") == ""
	if cfg.UI.Animations != nil && !*cfg.UI.Animations {
		anim = false
	}
	banner := tty && !flagNoBanner && !structured
	if cfg.UI.Banner != nil && !*cfg.UI.Banner {
		banner = false
	}
	ui.S = ui.Settings{Color: color, Animations: anim, Banner: banner, Plain: flagPlain, Light: cfg.UI.Theme == "light", Quiet: flagQuiet}
	return nil
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	code := api.ExitGeneric
	var ee *exitError
	if errors.As(err, &ee) {
		code = ee.code
	} else if errors.Is(err, auth.ErrNotLoggedIn) {
		code = api.ExitAuth
	} else if c := api.ExitCode(err); c != api.ExitGeneric {
		code = c
	} else if ctx.Err() != nil {
		code = api.ExitCancelled
	}
	reportError(err)
	return code
}

func reportError(err error) {
	if flagOutput == "json" || flagOutput == "ndjson" {
		var ae *api.Error
		e := map[string]string{"message": err.Error()}
		if errors.As(err, &ae) {
			e["code"], e["message"] = ae.Code, ae.Message
		}
		ui.Errf(`{"error":{"code":%q,"message":%q}}`, e["code"], e["message"])
		return
	}
	ui.Errf("%s", ui.Fail(err.Error()))
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagProfile, "profile", envOr("PTB_PROFILE", "default"), "profil akun")
	pf.StringVarP(&flagOutput, "output", "o", "", "format output: table, json, csv, ndjson")
	pf.BoolVar(&flagNoColor, "no-color", false, "tanpa warna")
	pf.BoolVar(&flagPlain, "plain", false, "teks polos tanpa glyph khusus dan animasi")
	pf.BoolVar(&flagNoAnimations, "no-animations", false, "tanpa animasi")
	pf.BoolVar(&flagNoBanner, "no-banner", false, "tanpa banner PTB")
	pf.DurationVar(&flagTimeout, "timeout", 60*time.Second, "batas waktu per permintaan")
	pf.Int64Var(&flagMaxTokens, "max-tokens", 0, "batas token per perintah, ditegakkan server (0 = tanpa batas)")
	pf.BoolVarP(&flagYes, "yes", "y", false, "langsung lanjutkan perintah berbayar tanpa konfirmasi")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "tanpa pesan informasi di stderr")
	pf.BoolVar(&flagVerbose, "verbose", false, "tampilkan detail permintaan di stderr")
	rootCmd.Version = version.Version
	rootCmd.SetVersionTemplate(version.String() + "\n")
	rootCmd.SetHelpCommandGroupID("")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// client returns an authenticated API client for the current profile.
func client(ctx context.Context) (*api.Client, auth.Source, error) {
	prof := cfg.Profile(flagProfile)
	bearer, src, err := auth.Bearer(ctx, flagProfile, prof)
	if err != nil {
		return nil, "", exitWith(api.ExitAuth, err)
	}
	if flagVerbose {
		ui.Errf("%s", ui.Dim(fmt.Sprintf("api=%s auth=%s profile=%s", prof.API, src, flagProfile)))
	}
	return api.New(prof.API, bearer, flagTimeout), src, nil
}

func format() ui.Format {
	f, _ := ui.ParseFormat(flagOutput)
	return f
}
