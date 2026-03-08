package notify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")

	data := `[
		{
			"id": "r1",
			"name": "Rule One",
			"enabled": true,
			"match": {"type": "service", "nameRegex": "^web-.*"},
			"webhook": "https://example.com/hook1",
			"cooldown": "5m"
		},
		{
			"id": "r2",
			"name": "Rule Two",
			"enabled": false,
			"match": {"type": "task", "action": "update"},
			"webhook": "https://example.com/hook2",
			"cooldown": "10s"
		}
	]`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	rules, err := LoadRules(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	if rules[0].ID != "r1" {
		t.Errorf("expected id r1, got %s", rules[0].ID)
	}
	if rules[0].nameRe == nil {
		t.Error("expected compiled regex on rule 0")
	}
	if !rules[0].nameRe.MatchString("web-api") {
		t.Error("expected regex to match web-api")
	}
	if rules[0].cooldownDur.Minutes() != 5 {
		t.Errorf("expected 5m cooldown, got %v", rules[0].cooldownDur)
	}

	if rules[1].ID != "r2" {
		t.Errorf("expected id r2, got %s", rules[1].ID)
	}
	if rules[1].cooldownDur.Seconds() != 10 {
		t.Errorf("expected 10s cooldown, got %v", rules[1].cooldownDur)
	}
}

func TestLoadRules_FileNotFound(t *testing.T) {
	rules, err := LoadRules("/tmp/nonexistent-cetacean-rules-12345.json")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if rules != nil {
		t.Fatalf("expected nil rules, got %v", rules)
	}
}

func TestLoadRules_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRules(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadRules_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-regex.json")

	data := `[{
		"id": "r1",
		"name": "Bad Regex",
		"enabled": true,
		"match": {"nameRegex": "[invalid("},
		"webhook": "https://example.com/hook",
		"cooldown": "1m"
	}]`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRules(path)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}
