package engine

import (
	"errors"
	"io/fs"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// DefaultConfigFile is the config file loaded from the working directory when
// no explicit path is given
const DefaultConfigFile = "./config/.config.yaml"

// envPrefix is the prefix for environment variable overrides
const envPrefix = "COURIER_"

const (
	// DefaultHost is the Openlane API host used when none is configured
	DefaultHost = "https://api.theopenlane.io"
	// DefaultDir is the store directory used when none is configured
	DefaultDir = "./data"
)

// Settings are the connection and store settings for the binary
type Settings struct {
	// Host is the base URL of the Openlane API
	Host string `koanf:"host"`
	// Token is the API token used for authentication
	Token string `koanf:"token" sensitive:"true"`
	// OrganizationID optionally scopes requests to a specific organization, only needed for multi-organization tokens
	OrganizationID string `koanf:"organization-id"`
	// Dir is the directory holding the exported files
	Dir string `koanf:"dir"`
}

// DefaultSettings are the settings used before any source is merged in
func DefaultSettings() Settings {
	return Settings{Host: DefaultHost, Dir: DefaultDir}
}

// LoadSettings merges settings from the config file at path (DefaultConfigFile
// when empty), COURIER_-prefixed environment variables, and set flags, later
// sources win. A missing default config file is not an error, a missing
// explicit one is
func LoadSettings(path string, flags *pflag.FlagSet) (Settings, error) {
	k := koanf.New(".")

	explicit := path != ""
	if !explicit {
		path = DefaultConfigFile
	}

	err := k.Load(file.Provider(path), yaml.Parser())

	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist) && !explicit:
	default:
		return Settings{}, err
	}

	envOpt := env.Opt{
		Prefix: envPrefix,
		TransformFunc: func(key, value string) (string, any) {
			return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(key, envPrefix)), "_", "-"), value
		},
	}

	if err := k.Load(env.Provider(".", envOpt), nil); err != nil {
		return Settings{}, err
	}

	if flags != nil {
		if err := k.Load(posflag.Provider(flags, k.Delim(), k), nil); err != nil {
			return Settings{}, err
		}
	}

	settings := DefaultSettings()
	if err := k.Unmarshal("", &settings); err != nil {
		return Settings{}, err
	}

	return settings, nil
}
