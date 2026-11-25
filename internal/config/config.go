// Package config stores non-secret settings per profile in the user config
// directory (~/.config/ptb/config.json on Linux, ~/Library/Application
// Support/ptb on macOS, %AppData%\ptb on Windows). Secrets live in the OS
// keychain (see package auth), never here.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

const (
	DefaultAPI    = "https://api.papertobillion.com/api/v2/mcp"
	DefaultIssuer = "https://auth.papertobillion.com"
	DefaultWeb    = "https://papertobillion.com"
)

// Profile is one account's settings.
type Profile struct {
	API      string `json:"api,omitempty"`
	Issuer   string `json:"issuer,omitempty"`
	ClientID string `json:"client_id,omitempty"` // OAuth client registered by `ptb login`
}

// UI is how the terminal output looks.
type UI struct {
	Theme      string `json:"theme,omitempty"` // dark | light
	Animations *bool  `json:"animations,omitempty"`
	Banner     *bool  `json:"banner,omitempty"`
	Color      *bool  `json:"color,omitempty"`
}

type File struct {
	Profiles map[string]*Profile `json:"profiles"`
	UI       UI                  `json:"ui"`
	Output   string              `json:"output,omitempty"` // default output format
}

func Dir() (string, error) {
	if d := os.Getenv("PTB_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ptb"), nil
}

func path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

func Load() (*File, error) {
	f := &File{Profiles: map[string]*Profile{}}
	p, err := path()
	if err != nil {
		return f, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, f); err != nil {
		return &File{Profiles: map[string]*Profile{}}, err
	}
	if f.Profiles == nil {
		f.Profiles = map[string]*Profile{}
	}
	return f, nil
}

func (f *File) Save() error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// Profile returns (creating) a profile with defaults and env overrides.
func (f *File) Profile(name string) *Profile {
	p := f.Profiles[name]
	if p == nil {
		p = &Profile{}
		f.Profiles[name] = p
	}
	out := *p
	if out.API == "" {
		out.API = DefaultAPI
	}
	if out.Issuer == "" {
		out.Issuer = DefaultIssuer
	}
	if v := os.Getenv("PTB_API_URL"); v != "" {
		out.API = v
	}
	if v := os.Getenv("PTB_AUTH_URL"); v != "" {
		out.Issuer = v
	}
	return &out
}

// SetClientID remembers the OAuth client of a profile.
func (f *File) SetClientID(profile, id string) {
	p := f.Profiles[profile]
	if p == nil {
		p = &Profile{}
		f.Profiles[profile] = p
	}
	p.ClientID = id
}

// Keys lists settable keys for `ptb config`.
var Keys = []string{"ui.theme", "ui.animations", "ui.banner", "ui.color", "output", "api", "issuer"}

func boolp(s string) (*bool, error) {
	switch s {
	case "true", "on", "1", "yes":
		v := true
		return &v, nil
	case "false", "off", "0", "no":
		v := false
		return &v, nil
	case "":
		return nil, nil
	}
	return nil, errors.New("nilai harus true atau false")
}

func strb(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "true"
	}
	return "false"
}

// Get returns a setting's value ("" when default).
func (f *File) Get(profile, key string) (string, error) {
	p := f.Profiles[profile]
	if p == nil {
		p = &Profile{}
	}
	switch key {
	case "ui.theme":
		return f.UI.Theme, nil
	case "ui.animations":
		return strb(f.UI.Animations), nil
	case "ui.banner":
		return strb(f.UI.Banner), nil
	case "ui.color":
		return strb(f.UI.Color), nil
	case "output":
		return f.Output, nil
	case "api":
		return p.API, nil
	case "issuer":
		return p.Issuer, nil
	}
	return "", errors.New("kunci tidak dikenal: " + key)
}

// Set changes a setting; "" resets it to the default.
func (f *File) Set(profile, key, val string) error {
	var err error
	switch key {
	case "ui.theme":
		if val != "" && val != "dark" && val != "light" {
			return errors.New("ui.theme: dark atau light")
		}
		f.UI.Theme = val
	case "ui.animations":
		f.UI.Animations, err = boolp(val)
	case "ui.banner":
		f.UI.Banner, err = boolp(val)
	case "ui.color":
		f.UI.Color, err = boolp(val)
	case "output":
		if val != "" && val != "table" && val != "json" && val != "csv" && val != "ndjson" {
			return errors.New("output: table, json, csv, atau ndjson")
		}
		f.Output = val
	case "api", "issuer":
		p := f.Profiles[profile]
		if p == nil {
			p = &Profile{}
			f.Profiles[profile] = p
		}
		if key == "api" {
			p.API = val
		} else {
			p.Issuer = val
		}
	default:
		return errors.New("kunci tidak dikenal: " + key)
	}
	return err
}

// List returns all keys and values, sorted.
func (f *File) List(profile string) [][2]string {
	keys := append([]string(nil), Keys...)
	sort.Strings(keys)
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		v, _ := f.Get(profile, k)
		out = append(out, [2]string{k, v})
	}
	return out
}
