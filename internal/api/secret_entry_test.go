package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/core"
)

type entryFixture struct {
	h       *Handlers
	entries *cluster.SecretEntries
	entry   cluster.SecretEntry
	created []swarm.SecretSpec
}

func newEntryFixture(t *testing.T) *entryFixture {
	t.Helper()

	shared := core.New(cache.New(nil), nil)
	f := &entryFixture{entries: shared.SecretEntries}

	wc := &mockWriteClient{
		createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
			f.created = append(f.created, spec)
			return "new-sec-id", nil
		},
	}
	f.h = newTestHandlers(t, withCore(shared), withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration))

	entry, err := f.entries.Open("alice", "db_password",
		map[string]string{"team": "shop"}, "utf8")
	if err != nil {
		t.Fatal(err)
	}
	f.entry = entry

	return f
}

func as(req *http.Request, subject string) *http.Request {
	return req.WithContext(
		auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: subject}),
	)
}

func (f *entryFixture) get(subject, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/secret-entries/"+id, nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	f.h.HandleGetSecretEntry(w, as(req, subject))

	return w
}

func (f *entryFixture) create(subject, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/secrets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.h.HandleCreateSecret(w, as(req, subject))

	return w
}

func TestGetSecretEntry_ServesTheOwnerTheEntry(t *testing.T) {
	f := newEntryFixture(t)

	w := f.get("alice", f.entry.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d; body: %s", w.Code, w.Body.String())
	}

	var body struct {
		Name     string            `json:"name"`
		Labels   map[string]string `json:"labels"`
		Encoding string            `json:"encoding"`
		Type     string            `json:"@type"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body.Name != "db_password" || body.Labels["team"] != "shop" || body.Encoding != "utf8" ||
		body.Type != "SecretEntry" {
		t.Errorf("body = %s", w.Body.String())
	}

	if allow := w.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q", allow)
	}
}

func TestGetSecretEntry_RefusesSomeoneElse(t *testing.T) {
	f := newEntryFixture(t)

	if w := f.get("mallory", f.entry.ID); w.Code != http.StatusForbidden ||
		!strings.Contains(w.Body.String(), "SEC007") ||
		strings.Contains(w.Body.String(), "db_password") {
		t.Errorf("status=%d; body: %s", w.Code, w.Body.String())
	}

	if w := f.get("alice", "unknown"); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "SEC006") {
		t.Errorf("unknown entry: status=%d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateSecret_CompletesTheEntry(t *testing.T) {
	f := newEntryFixture(t)

	w := f.create("alice", `{"data":"c2VjcmV0","elicitation":"`+f.entry.ID+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d; body: %s", w.Code, w.Body.String())
	}

	if len(f.created) != 1 || f.created[0].Name != "db_password" ||
		f.created[0].Labels["team"] != "shop" || string(f.created[0].Data) != "secret" {
		t.Fatalf("created = %+v", f.created)
	}

	entry, err := f.entries.For(f.entry.ID, "alice")
	if err != nil || entry.SecretID() != "new-sec-id" {
		t.Errorf("entry = %+v, %v; want completed", entry, err)
	}
}

func TestCreateSecret_EntryOfSomeoneElseIsRefused(t *testing.T) {
	f := newEntryFixture(t)

	w := f.create("mallory", `{"data":"c2VjcmV0","elicitation":"`+f.entry.ID+`"}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "SEC007") {
		t.Errorf("status=%d; body: %s", w.Code, w.Body.String())
	}

	if len(f.created) != 0 {
		t.Errorf("created %+v for the wrong person", f.created)
	}
}

func TestCreateSecret_EntryNameMustMatch(t *testing.T) {
	f := newEntryFixture(t)

	w := f.create("alice", `{"name":"other","data":"c2VjcmV0","elicitation":"`+f.entry.ID+`"}`)
	if w.Code != http.StatusBadRequest || len(f.created) != 0 {
		t.Errorf("status=%d, created %+v; body: %s", w.Code, f.created, w.Body.String())
	}
}

func TestCreateConfig_RefusesAnElicitation(t *testing.T) {
	f := newEntryFixture(t)

	req := httptest.NewRequest("POST", "/configs",
		strings.NewReader(`{"name":"x","data":"c2VjcmV0","elicitation":"`+f.entry.ID+`"}`))
	w := httptest.NewRecorder()
	f.h.HandleCreateConfig(w, as(req, "alice"))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d; body: %s", w.Code, w.Body.String())
	}
}
