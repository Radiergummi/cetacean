package cluster

import (
	"reflect"
	"slices"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestServiceLinks(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   []ServiceLink
	}{
		{"no labels", nil, nil},
		{"unrelated labels", map[string]string{"traefik.enable": "true"}, nil},
		{
			"https with title",
			map[string]string{
				"cetacean.link.grafana":       "https://grafana.example.com/d/abc",
				"cetacean.link.grafana.title": "Dashboard",
			},
			[]ServiceLink{
				{ID: "grafana", Title: "Dashboard", Href: "https://grafana.example.com/d/abc"},
			},
		},
		{
			"title defaults to id",
			map[string]string{"cetacean.link.runbook": "http://wiki.example.com/runbook"},
			[]ServiceLink{
				{ID: "runbook", Title: "runbook", Href: "http://wiki.example.com/runbook"},
			},
		},
		{
			"uppercase scheme is a link",
			map[string]string{"cetacean.link.repo": "HTTPS://git.example.com/repo"},
			[]ServiceLink{{ID: "repo", Title: "repo", Href: "HTTPS://git.example.com/repo"}},
		},
		{
			"non-http scheme is text",
			map[string]string{"cetacean.link.x": "javascript:alert(1)"},
			[]ServiceLink{{ID: "x", Title: "x", Text: "javascript:alert(1)"}},
		},
		{
			"schemeless value is text",
			map[string]string{"cetacean.link.x": "grafana.example.com"},
			[]ServiceLink{{ID: "x", Title: "x", Text: "grafana.example.com"}},
		},
		{
			"empty hostname is text",
			map[string]string{"cetacean.link.x": "https://:8080/path"},
			[]ServiceLink{{ID: "x", Title: "x", Text: "https://:8080/path"}},
		},
		{
			"href is normalised",
			map[string]string{"cetacean.link.x": "https://wiki.example.com/Bäckerei"},
			[]ServiceLink{{ID: "x", Title: "x", Href: "https://wiki.example.com/B%C3%A4ckerei"}},
		},
		{
			"userinfo is text",
			map[string]string{"cetacean.link.x": "https://grafana.corp.example@attacker.example/"},
			[]ServiceLink{
				{ID: "x", Title: "x", Text: "https://grafana.corp.example@attacker.example/"},
			},
		},
		{
			"title is trimmed",
			map[string]string{
				"cetacean.link.x":       "https://example.com",
				"cetacean.link.x.title": "  Dashboard\t",
			},
			[]ServiceLink{{ID: "x", Title: "Dashboard", Href: "https://example.com"}},
		},
		{
			"whitespace-only title falls back to id",
			map[string]string{
				"cetacean.link.x":       "https://example.com",
				"cetacean.link.x.title": " \n ",
			},
			[]ServiceLink{{ID: "x", Title: "x", Href: "https://example.com"}},
		},
		{
			"percent-encoded url is a link",
			map[string]string{"cetacean.link.x": "https://example.com/a%20b?q=1#top"},
			[]ServiceLink{{ID: "x", Title: "x", Href: "https://example.com/a%20b?q=1#top"}},
		},
		{
			"surrounding whitespace is trimmed",
			map[string]string{"cetacean.link.x": "  https://example.com/a \n"},
			[]ServiceLink{{ID: "x", Title: "x", Href: "https://example.com/a"}},
		},
		{
			"whitespace-only value is ignored",
			map[string]string{"cetacean.link.x": " \t ", "cetacean.link.x.title": "X"},
			nil,
		},
		{
			"title without url is ignored",
			map[string]string{"cetacean.link.orphan.title": "Orphan"},
			nil,
		},
		{
			"empty value is ignored",
			map[string]string{"cetacean.link.empty": "", "cetacean.link.empty.title": "Empty"},
			nil,
		},
		{
			"empty id is ignored",
			map[string]string{"cetacean.link.": "https://example.com"},
			nil,
		},
		{
			"empty title falls back to id",
			map[string]string{
				"cetacean.link.a":       "https://a.example.com",
				"cetacean.link.a.title": "",
			},
			[]ServiceLink{{ID: "a", Title: "a", Href: "https://a.example.com"}},
		},
		{
			"sorted by id",
			map[string]string{
				"cetacean.link.zeta":  "https://z.example.com",
				"cetacean.link.alpha": "https://a.example.com",
				"cetacean.link.mid":   "https://m.example.com",
			},
			[]ServiceLink{
				{ID: "alpha", Title: "alpha", Href: "https://a.example.com"},
				{ID: "mid", Title: "mid", Href: "https://m.example.com"},
				{ID: "zeta", Title: "zeta", Href: "https://z.example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ServiceLinks(tt.labels); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// An empty want means the value is not structurally an http(s) URL and stays
// text; otherwise want is the href, a valid RFC 3986 URI.
func TestNormaliseHTTPURL(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com/", "http://example.com/"},
		{"HTTPS://Example.COM/a/b", "HTTPS://Example.COM/a/b"},
		{
			"https://example.com/a%20b?q=1&r=a/b?c#top/x?y",
			"https://example.com/a%20b?q=1&r=a/b?c#top/x?y",
		},
		{"https://example.com:8443/p", "https://example.com:8443/p"},
		{"https://example.com:65535/", "https://example.com:65535/"},
		{"https://example.com:/p", "https://example.com:/p"},
		{"https://[2001:db8::1]:8080/p", "https://[2001:db8::1]:8080/p"},
		{"https://[::1]/", "https://[::1]/"},
		{"https://example.com/a:b@c!$&'()*+,;=-._~", "https://example.com/a:b@c!$&'()*+,;=-._~"},
		{
			`https://grafana.example.com/explore?left={"queries":[{"expr":"{job=\"web\"}"}]}`,
			"https://grafana.example.com/explore?left=" +
				"%7B%22queries%22:%5B%7B%22expr%22:%22%7Bjob=%5C%22web%5C%22%7D%22%7D%5D%7D",
		},
		{
			`https://prometheus.example.com/graph?g0.expr=up{job="web"}`,
			"https://prometheus.example.com/graph?g0.expr=up%7Bjob=%22web%22%7D",
		},
		{"https://wiki.example.com/Bäckerei", "https://wiki.example.com/B%C3%A4ckerei"},
		{"https://example.com/a b", "https://example.com/a%20b"},
		{"https://example.com/a\x7f", "https://example.com/a%7F"},
		{"https://example.com/a[b]", "https://example.com/a%5Bb%5D"},
		{"https://example.com/#a#b", "https://example.com/#a%23b"},
		{"https://example.com/?q=%zz", "https://example.com/?q=%25zz"},
		{"https://example.com/%2", "https://example.com/%252"},
		{"https://example.com/%", "https://example.com/%25"},
		{"https://example.com/|^`<>\\", "https://example.com/%7C%5E%60%3C%3E%5C"},

		{"ftp://example.com", ""},
		{"javascript:alert(1)", ""},
		{"https:example.com", ""},
		{"https://", ""},
		{"https:///path", ""},
		{"https://:8080/path", ""},
		{"https://user@example.com/", ""},
		{"https://grafana.corp.example@attacker.example/", ""},
		{"https://ex[ample.com/", ""},
		{"https://exa mple.com/", ""},
		{"https://bäckerei.example/", "https://xn--bckerei-5wa.example/"},
		{"https://Bäckerei.Example:8443/a", "https://xn--bckerei-5wa.example:8443/a"},
		{"https://grafana.corp.example%2Eattacker.example/", ""},
		{"https://example%2ecom/", ""},
		{"https://ex%61mple.com/", ""},
		{"https://xn--a.example/", "https://xn--a.example/"},
		{"https://\u0301ab.example/", ""},
		{"https://a\u200b\u0301b.example/", "https://xn--b-tfa.example/"},
		{"https://2001:db8::1/", ""},
		{"https://[example.com]/", ""},
		{"https://[192.0.2.1]/", ""},
		{"https://[fe80::1%25en0]/", ""},
		{"https://[::1/", ""},
		{"https://[::1]x/", ""},
		{"https://example.com:80a/", ""},
		{"https://example.com:80:81/", ""},
		{"https://example.com:65536/", ""},
		{"https://example.com:99999999999999999999/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, ok := normaliseHTTPURL(tt.value)
			if ok != (tt.want != "") || got != tt.want {
				t.Errorf("normaliseHTTPURL(%q) = %q, %v; want %q", tt.value, got, ok, tt.want)
			}
		})
	}
}

func TestStackLinks(t *testing.T) {
	service := func(name string, labels map[string]string) swarm.Service {
		return swarm.Service{
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name, Labels: labels}},
		}
	}

	services := []swarm.Service{
		service("app_api", map[string]string{
			"cetacean.link.repo":    "https://git.example.com/app",
			"cetacean.link.grafana": "https://grafana.example.com/api",
		}),
		service("app_worker", map[string]string{
			"cetacean.link.repo":    "https://git.example.com/app",
			"cetacean.link.grafana": "https://grafana.example.com/worker",
		}),
		service("app_docs", map[string]string{
			"cetacean.link.source":       "https://git.example.com/app",
			"cetacean.link.source.title": "repo",
		}),
		service("app_cron", map[string]string{
			"cetacean.link.repo":       "https://git.example.com/app",
			"cetacean.link.repo.title": "Code",
		}),
		service("app_db", nil),
	}

	want := []ServiceLink{
		{
			ID: "grafana", Title: "grafana", Href: "https://grafana.example.com/api",
			Services: []string{"app_api"},
		},
		{
			ID: "grafana", Title: "grafana", Href: "https://grafana.example.com/worker",
			Services: []string{"app_worker"},
		},
		{
			ID: "repo", Title: "Code", Href: "https://git.example.com/app",
			Services: []string{"app_cron"},
		},
		{
			ID: "repo", Title: "repo", Href: "https://git.example.com/app",
			Services: []string{"app_api", "app_worker"},
		},
		{
			ID: "source", Title: "repo", Href: "https://git.example.com/app",
			Services: []string{"app_docs"},
		},
	}

	if got := StackLinks(services); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// The member order must not change the answer, or the ETag would churn.
	slices.Reverse(services)

	if got := StackLinks(services); !reflect.DeepEqual(got, want) {
		t.Fatalf("reversed members: got %+v, want %+v", got, want)
	}
}

func TestDigestsCarryLinks(t *testing.T) {
	svc := swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
		Name:   "web",
		Labels: map[string]string{"cetacean.link.repo": "https://git.example.com/web"},
	}}}
	want := []ServiceLink{{ID: "repo", Title: "repo", Href: "https://git.example.com/web"}}

	if got := ServiceDetails(svc)["links"]; !reflect.DeepEqual(got, want) {
		t.Errorf("service links = %+v, want %+v", got, want)
	}

	want[0].Services = []string{"web"}

	stack := StackDigest(cache.StackDetail{Name: "demo", Services: []swarm.Service{svc}}, nil, nil)
	if got := stack.Details["links"]; !reflect.DeepEqual(got, want) {
		t.Errorf("stack links = %+v, want %+v", got, want)
	}
}

func TestDigestsOmitLinksWhenNone(t *testing.T) {
	svc := swarm.Service{ID: "svc1"}

	if _, present := ServiceDetails(svc)["links"]; present {
		t.Error("service links present with no link labels")
	}

	stack := StackDigest(cache.StackDetail{Name: "demo", Services: []swarm.Service{svc}}, nil, nil)
	if _, present := stack.Details["links"]; present {
		t.Error("stack links present with no link labels")
	}
}
