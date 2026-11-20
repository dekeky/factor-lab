package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsMigratesLegacy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	raw := `{
  "base_url": "https://api.deepseek.com/v1",
  "api_key": "sk-test-key",
  "model": "deepseek-v4-flash",
  "temperature": 0.3,
  "timeout_sec": 90
}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ok, err := LoadSettings(path)
	if err != nil || !ok {
		t.Fatalf("LoadSettings: ok=%v err=%v", ok, err)
	}
	if len(s.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(s.Models))
	}
	m := s.Models[0]
	if m.Model != "deepseek-v4-flash" || m.APIKey != "sk-test-key" || m.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("migrated entry mismatch: %+v", m)
	}
	if s.ActiveID != m.ID || s.Model != m.Model || s.APIKey != m.APIKey {
		t.Fatalf("active not synced: %+v", s)
	}
}

func TestNormalizeMultipleModels(t *testing.T) {
	s := Settings{
		Temperature: 0.2,
		TimeoutSec:  30,
		ActiveID:    "b",
		Models: []ModelEntry{
			{ID: "a", Name: "A", BaseURL: "https://a.example/v1", APIKey: "ka", Model: "ma"},
			{ID: "b", Name: "B", BaseURL: "https://b.example/v1", APIKey: "kb", Model: "mb"},
		},
	}.Normalize()
	if s.Model != "mb" || s.BaseURL != "https://b.example/v1" || s.APIKey != "kb" {
		t.Fatalf("active should be b: %+v", s)
	}
	act, ok := s.Active()
	if !ok || act.ID != "b" {
		t.Fatalf("Active() = %+v ok=%v", act, ok)
	}
}

func TestEmptyModelsIsUnconfigured(t *testing.T) {
	s := Settings{Models: []ModelEntry{}, Temperature: 0.3, TimeoutSec: 90}.Normalize()
	if s.BaseURL != "" || s.Model != "" || s.ActiveID != "" {
		t.Fatalf("empty models should clear conn: %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NewLLM(t.Context()); err == nil {
		t.Fatal("empty models should not construct LLM")
	}
}
