package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Server   ServerConfig   `json:"server"`
	Database DatabaseConfig `json:"database"`
	Library  LibraryConfig  `json:"library"`
	Transcode TranscodeConfig `json:"transcode"`
	Scrobble ScrobbleConfig `json:"scrobble"`
	Auth     AuthConfig     `json:"auth"`
}

type ServerConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	BaseURL      string `json:"base_url"`
	StaticDir    string `json:"static_dir"`
}

type DatabaseConfig struct {
	Path string `json:"path"`
}

type LibraryConfig struct {
	Paths        []string `json:"paths"`
	ScanInterval int      `json:"scan_interval_minutes"`
}

type TranscodeConfig struct {
	DefaultBitrate  int  `json:"default_bitrate"`
	MaxConcurrency  int  `json:"max_concurrency"`
	FFmpegPath      string `json:"ffmpeg_path"`
}

type ScrobbleConfig struct {
	LastFM      LastFMConfig      `json:"lastfm"`
	ListenBrainz ListenBrainzConfig `json:"listenbrainz"`
}

type LastFMConfig struct {
	Enabled   bool   `json:"enabled"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
}

type ListenBrainzConfig struct {
	Enabled   bool   `json:"enabled"`
	BaseURL   string `json:"base_url"`
}

type AuthConfig struct {
	OIDCEnabled bool   `json:"oidc_enabled"`
	OIDCIssuer  string `json:"oidc_issuer"`
	OIDCClientID string `json:"oidc_client_id"`
}

func Default() *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".crate")

	return &Config{
		Server: ServerConfig{
			Host:      "0.0.0.0",
			Port:      4533,
			BaseURL:   "/",
			StaticDir: "web/static",
		},
		Database: DatabaseConfig{
			Path: filepath.Join(dataDir, "crate.db"),
		},
		Library: LibraryConfig{
			Paths:        []string{filepath.Join(home, "Music")},
			ScanInterval: 5,
		},
		Transcode: TranscodeConfig{
			DefaultBitrate: 192,
			MaxConcurrency: 3,
			FFmpegPath:     "ffmpeg",
		},
		Scrobble: ScrobbleConfig{
			LastFM: LastFMConfig{Enabled: false},
			ListenBrainz: ListenBrainzConfig{
				Enabled: true,
				BaseURL: "https://api.listenbrainz.org",
			},
		},
		Auth: AuthConfig{},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return cfg, nil
			}
			return nil, err
		}
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0755); err != nil {
		return nil, err
	}

	return cfg, nil
}

func Save(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
