package integrations

import "testing"

// Traefik matches label keys in any case, so the panel must too, while still
// showing names as they were written.
func TestDetectTraefik_KeysInAnyCase(t *testing.T) {
	result := detectTraefik(map[string]string{
		"Traefik.HTTP.Routers.Web.Rule":                           "Host(`example.com`)",
		"traefik.http.routers.Web.TLS.CertResolver":               "le",
		"TRAEFIK.http.Services.Web.LoadBalancer.Server.Port":      "8080",
		"traefik.HTTP.middlewares.Redirect.RedirectScheme.scheme": "https",
	})
	if result == nil {
		t.Fatal("no Traefik integration detected")
	}

	if len(result.Routers) != 1 {
		t.Fatalf("routers = %+v, want one", result.Routers)
	}
	router := result.Routers[0]
	if router.Name != "Web" || router.Rule != "Host(`example.com`)" {
		t.Errorf("router = %+v, want Web with its rule", router)
	}
	if router.TLS == nil || router.TLS.CertResolver != "le" {
		t.Errorf("router TLS = %+v, want certresolver le", router.TLS)
	}

	if len(result.Services) != 1 || result.Services[0].Port != 8080 {
		t.Errorf("services = %+v, want Web on 8080", result.Services)
	}

	if len(result.Middlewares) != 1 {
		t.Fatalf("middlewares = %+v, want one", result.Middlewares)
	}
	mw := result.Middlewares[0]
	if mw.Name != "Redirect" || mw.Type != "RedirectScheme" || mw.Config["scheme"] != "https" {
		t.Errorf("middleware = %+v, want Redirect as written", mw)
	}
}

// traefik.enable is a Go boolean, in a key of any case.
func TestDetectTraefik_EnableIsABoolean(t *testing.T) {
	cases := []struct {
		key, value string
		want       bool
	}{
		{"traefik.enable", "true", true},
		{"traefik.enable", "1", true},
		{"traefik.enable", "True", true},
		{"Traefik.Enable", "TRUE", true},
		{"traefik.enable", "false", false},
		{"TRAEFIK.ENABLE", "0", false},
		{"traefik.enable", "yes", false},
	}

	for _, tc := range cases {
		result := detectTraefik(map[string]string{tc.key: tc.value})
		if result == nil {
			t.Fatalf("%s=%s: no Traefik integration detected", tc.key, tc.value)
		}
		if result.Enabled != tc.want {
			t.Errorf("%s=%s: enabled = %v, want %v", tc.key, tc.value, result.Enabled, tc.want)
		}
	}
}
