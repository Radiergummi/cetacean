package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
