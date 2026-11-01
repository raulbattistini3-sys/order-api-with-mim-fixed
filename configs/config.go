// Package configs loads the settings both commands share. Values come from the
// built-in defaults, then config.toml, then the environment, so one file can
// serve several processes that differ by a port or a rate. Secrets are read
// from the environment only and never belong in config.toml.
package configs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is every setting of the API and the gateway.
type Config struct {
	API        API        `toml:"api"`
	Gateway    Gateway    `toml:"gateway"`
	PocketBase PocketBase `toml:"pocketbase"`
	Limits     Limits     `toml:"limits"`

	ClientSecret string `toml:"-"`
}

// API configures cmd/api.
type API struct {
	Port           string `toml:"port"`
	ImportWorkers  int    `toml:"import_workers"`
	ImportSyncRows int    `toml:"import_sync_rows"` // larger files go to the queue
	ImportSpoolDir string `toml:"import_spool_dir"` // queued files wait here
	CNPJAPIURL     string `toml:"cnpj_api_url"`
}

// Gateway configures cmd/gateway.
type Gateway struct {
	Port            string        `toml:"port"`
	Upstream        string        `toml:"upstream"`
	Rate            float64       `toml:"rate"`
	Burst           int           `toml:"burst"`
	TrustProxy      bool          `toml:"trust_proxy"`
	SignatureMaxAge time.Duration `toml:"signature_max_age"`
}

// PocketBase says where the database is and how to log in to it.
type PocketBase struct {
	URL      string `toml:"url"`
	Email    string `toml:"-"`
	Password string `toml:"-"`
}

// Limits caps request bodies, in MiB.
type Limits struct {
	MaxBodyMB   int64 `toml:"max_body_mb"`
	MaxImportMB int64 `toml:"max_import_mb"`
}

// BodyBytes is the cap on every route, in bytes.
func (l Limits) BodyBytes() int64 { return l.MaxBodyMB << 20 }

// ImportBytes is the cap on the spreadsheet import route, in bytes.
func (l Limits) ImportBytes() int64 { return l.MaxImportMB << 20 }

func defaults() Config {
	return Config{
		API:        API{Port: "8080", ImportWorkers: 16, ImportSyncRows: 15_000, ImportSpoolDir: "bin/imports", CNPJAPIURL: "https://brasilapi.com.br"},
		Gateway:    Gateway{Port: "8081", Upstream: "http://localhost:8080", Rate: 100, Burst: 200, SignatureMaxAge: 30 * time.Second},
		PocketBase: PocketBase{URL: "http://localhost:8090"},
		Limits:     Limits{MaxBodyMB: 1, MaxImportMB: 96},
	}
}

// Load reads config.toml, or the file named by CONFIG_FILE. A missing file is
// fine, but a key it doesn't know is an error, so a typo can't be ignored.
func Load() (Config, error) {
	cfg := defaults()
	path := os.Getenv("CONFIG_FILE")
	if path == "" {
		path = "config.toml"
	}
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		keys := make([]string, len(unknown))
		for i, key := range unknown {
			keys[i] = key.String()
		}
		return Config{}, fmt.Errorf("%s: unknown keys: %s", path, strings.Join(keys, ", "))
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.Limits.MaxBodyMB <= 0 || cfg.Limits.MaxImportMB <= 0 {
		return Config{}, errors.New("limits must be a positive number of MiB")
	}
	return cfg, nil
}

func applyEnv(cfg *Config) error {
	var errs []error
	text := func(s string) (string, error) { return s, nil }
	megabytes := func(s string) (int64, error) { return strconv.ParseInt(s, 10, 32) }

	override(&errs, "PORT", &cfg.API.Port, text)
	override(&errs, "PORT", &cfg.Gateway.Port, text)
	override(&errs, "IMPORT_WORKERS", &cfg.API.ImportWorkers, strconv.Atoi)
	override(&errs, "IMPORT_SYNC_ROWS", &cfg.API.ImportSyncRows, strconv.Atoi)
	override(&errs, "IMPORT_SPOOL_DIR", &cfg.API.ImportSpoolDir, text)
	override(&errs, "CNPJ_API_URL", &cfg.API.CNPJAPIURL, text)
	override(&errs, "UPSTREAM", &cfg.Gateway.Upstream, text)
	override(&errs, "GATEWAY_RATE", &cfg.Gateway.Rate, func(s string) (float64, error) { return strconv.ParseFloat(s, 64) })
	override(&errs, "GATEWAY_BURST", &cfg.Gateway.Burst, strconv.Atoi)
	override(&errs, "TRUST_PROXY", &cfg.Gateway.TrustProxy, strconv.ParseBool)
	override(&errs, "SIGNATURE_MAX_AGE", &cfg.Gateway.SignatureMaxAge, time.ParseDuration)
	override(&errs, "PB_URL", &cfg.PocketBase.URL, text)
	override(&errs, "PB_EMAIL", &cfg.PocketBase.Email, text)
	override(&errs, "PB_PASSWORD", &cfg.PocketBase.Password, text)
	override(&errs, "MAX_BODY_MB", &cfg.Limits.MaxBodyMB, megabytes)
	override(&errs, "MAX_IMPORT_MB", &cfg.Limits.MaxImportMB, megabytes)
	override(&errs, "CLIENT_SECRET", &cfg.ClientSecret, text)
	return errors.Join(errs...)
}

// override replaces dst with the environment variable key when it is set.
func override[T any](errs *[]error, key string, dst *T, parse func(string) (T, error)) {
	v := os.Getenv(key)
	if v == "" {
		return
	}
	parsed, err := parse(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %w", key, err))
		return
	}
	*dst = parsed
}
