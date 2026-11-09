package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPrecedenceAndErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("API_KEY='file-secret'\nBASE_URL='http://localhost:1234/v1/'\nMODEL='file-model'\n"), 0600)
	t.Setenv("TOSH_API_KEY", "env-secret")
	t.Setenv("TOSH_BASE_URL", "")
	t.Setenv("TOSH_MODEL", "")
	c, e := Load(p)
	if e != nil || c.APIKey != "env-secret" || c.Model != "file-model" || c.BaseURL != "http://localhost:1234/v1" {
		t.Fatalf("unexpected config: %v", e)
	}
	os.WriteFile(p, []byte("API_KEY='DO-NOT-LEAK\n"), 0600)
	_, e = Load(p)
	if e == nil || strings.Contains(e.Error(), "DO-NOT-LEAK") {
		t.Fatal("malformed config must fail without secrets")
	}
}
func TestInitDoesNotOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tosh", "config.toml")
	if e := Init(p); e != nil {
		t.Fatal(e)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	if Init(p) == nil {
		t.Fatal("must refuse overwrite")
	}
}

func TestLanguageConfigAndEnvironment(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("API_KEY='test'\nBASE_URL='http://localhost/v1'\nMODEL='test'\nLANGUAGE='zh'\n"), 0600)
	t.Setenv("TOSH_LANG", "")
	if LanguagePreference(p) != "zh" {
		t.Fatal("config language ignored")
	}
	c, e := Load(p)
	if e != nil || c.Language != "zh" {
		t.Fatal("language not loaded")
	}
	t.Setenv("TOSH_LANG", "en")
	c, e = Load(p)
	if e != nil || c.Language != "en" || LanguagePreference(p) != "en" {
		t.Fatal("environment did not override language")
	}
}
