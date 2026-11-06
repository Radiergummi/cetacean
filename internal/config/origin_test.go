package config

import "testing"

func TestSerialiseOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://A.example":      "https://a.example",
		"HTTPS://a.example:443":  "https://a.example",
		"http://a.example:80":    "http://a.example",
		"http://a.example:8080":  "http://a.example:8080",
		"https://a.example:80":   "https://a.example:80",
		"http://[::1]:9000":      "http://[::1]:9000",
		"*":                      "*",
		"null":                   "null",
		"https://a.example/path": "https://a.example/path",
	} {
		if got := SerialiseOrigin(in); got != want {
			t.Errorf("SerialiseOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}
