package config

import (
	"github.com/pelletier/go-toml/v2"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"tosh/internal/locale"
)

type Config struct {
	Language string            `toml:"LANGUAGE"`
	APIKey   string            `toml:"API_KEY"`
	BaseURL  string            `toml:"BASE_URL"`
	Model    string            `toml:"MODEL"`
	Shell    string            `toml:"SHELL"`
	Sources  map[string]string `toml:"-"`
}

func DefaultPath() string {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "tosh", "config.toml")
}

func Load(path string) (Config, error) {
	c := Config{Sources: map[string]string{}}
	explicit := path != ""
	if !explicit {
		path = DefaultPath()
	}
	b, err := os.ReadFile(path)
	if err == nil {
		if toml.Unmarshal(b, &c) != nil {
			return c, locale.Fail("Invalid TOML configuration: %s", path)
		}
		for _, k := range []string{"API_KEY", "BASE_URL", "MODEL"} {
			c.Sources[k] = path
		}
	} else if explicit || !os.IsNotExist(err) {
		return c, locale.Fail("Cannot read configuration: %s", path)
	}
	for k, p := range map[string]*string{"API_KEY": &c.APIKey, "BASE_URL": &c.BaseURL, "MODEL": &c.Model, "SHELL": &c.Shell, "LANG": &c.Language} {
		if v := os.Getenv("TOSH_" + k); v != "" {
			*p = v
			c.Sources[k] = "TOSH_" + k
		}
	}
	if c.APIKey == "" || c.BaseURL == "" || c.Model == "" {
		return c, locale.Fail("Set API_KEY, BASE_URL, and MODEL in %s, or use TOSH_ environment variables", path)
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, locale.Fail("BASE_URL must be an HTTP(S) API root without credentials, query, or fragment")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return c, nil
}

func Init(path string) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return locale.Fail("Configuration already exists or cannot be created: %s", path)
	}
	defer f.Close()
	_, err = f.WriteString("API_KEY = \"replace-with-your-key\"\nBASE_URL = \"https://api.openai.com/v1\"\nMODEL = \"your-model-id\"\nLANGUAGE = \"en\"\n")
	return err
}

// LanguagePreference 供 -h 在无需认证和完整配置的情况下选择语言。
func LanguagePreference(path string) string {
	if value := os.Getenv("TOSH_LANG"); value != "" {
		return value
	}
	if path == "" {
		path = DefaultPath()
	}
	var c struct {
		Language string `toml:"LANGUAGE"`
	}
	if b, e := os.ReadFile(path); e == nil {
		_ = toml.Unmarshal(b, &c)
	}
	return c.Language
}
