package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cluster"
)

func TestPreferMinimal(t *testing.T) {
	tests := []struct {
		name   string
		header []string
		want   bool
	}{
		{
			name: "no prefer header",
			want: false,
		},
		{
			name:   "return=minimal",
			header: []string{"return=minimal"},
			want:   true,
		},
		{
			name:   "return=representation",
			header: []string{"return=representation"},
			want:   false,
		},
		{
			name:   "multiple tokens comma-separated",
			header: []string{"respond-async, return=minimal"},
			want:   true,
		},
		{
			name:   "multiple header values",
			header: []string{"respond-async", "return=minimal"},
			want:   true,
		},
		{
			name:   "whitespace around token",
			header: []string{" return=minimal "},
			want:   true,
		},
		{
			name:   "unrelated preference",
			header: []string{"respond-async"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("PUT", "/", nil)
			for _, v := range tt.header {
				r.Header.Add("Prefer", v)
			}

			if got := preferMinimal(r); got != tt.want {
				t.Errorf("preferMinimal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWritePreferMinimal(t *testing.T) {
	w := httptest.NewRecorder()
	writePreferMinimal(w)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	if got := w.Header().Get("Preference-Applied"); got != "return=minimal" {
		t.Errorf("Preference-Applied = %q, want %q", got, "return=minimal")
	}

	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", w.Body.String())
	}
}

func TestPreferWait(t *testing.T) {
	cases := []struct {
		name   string
		header []string
		want   time.Duration
		ok     bool
	}{
		{"absent", nil, 0, false},
		{"simple", []string{"wait=30"}, 30 * time.Second, true},
		{"with other tokens", []string{"respond-async, wait=10"}, 10 * time.Second, true},
		{"separate fields", []string{"return=minimal", "wait=5"}, 5 * time.Second, true},
		{"spaces around equals", []string{"wait = 7"}, 7 * time.Second, true},
		{"clamped to the ceiling", []string{"wait=100000"}, cluster.ConvergenceTimeout, true},
		{"zero is honoured", []string{"wait=0"}, 0, true},
		{"non-numeric is ignored", []string{"wait=soon"}, 0, false},
		{"negative is ignored", []string{"wait=-5"}, 0, false},
		{"quoted value", []string{`wait="30"`}, 30 * time.Second, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("PUT", "/services/svc1/scale", nil)
			for _, v := range tc.header {
				r.Header.Add("Prefer", v)
			}

			got, ok := preferWait(r)
			if ok != tc.ok || got != tc.want {
				t.Errorf("preferWait() = (%v, %v), want (%v, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestPreferRespondAsync(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"respond-async", true},
		{"respond-async, wait=10", true},
		{"return=minimal", false},
		{"", false},
	}

	for _, tc := range cases {
		r := httptest.NewRequest("PUT", "/services/svc1/scale", nil)
		if tc.header != "" {
			r.Header.Set("Prefer", tc.header)
		}
		if got := preferRespondAsync(r); got != tc.want {
			t.Errorf("preferRespondAsync(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}
