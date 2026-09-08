package main

import (
	"encoding/json"
	"testing"
)

// The website reads these key names and renders every one of these fields, so
// a rename or an unpopulated field is a broken page rather than a failed build.
func TestEncodeProducesBothHalves(t *testing.T) {
	data, err := encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var catalog struct {
		Domains []struct {
			Prefix string `json:"prefix"`
			Label  string `json:"label"`
		} `json:"domains"`
		Errors []struct {
			Code        string `json:"code"`
			Title       string `json:"title"`
			Status      int    `json:"status"`
			Description string `json:"description"`
			Suggestion  string `json:"suggestion"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(catalog.Domains) == 0 {
		t.Error("no domains in output")
	}
	if len(catalog.Errors) == 0 {
		t.Error("no errors in output")
	}

	for _, domain := range catalog.Domains {
		if domain.Prefix == "" || domain.Label == "" {
			t.Errorf("domain %+v has an empty field", domain)
		}
	}

	for _, def := range catalog.Errors {
		if def.Code == "" || def.Title == "" || def.Status == 0 ||
			def.Description == "" || def.Suggestion == "" {
			t.Errorf("error %q has an empty field: %+v", def.Code, def)
		}
	}
}
