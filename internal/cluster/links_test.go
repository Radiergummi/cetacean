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
			"whitespace is text",
			map[string]string{"cetacean.link.x": "https://example.com/a b"},
			[]ServiceLink{{ID: "x", Title: "x", Text: "https://example.com/a b"}},
		},
		{
			"control character is text",
			map[string]string{"cetacean.link.x": "https://example.com/a\x7f"},
			[]ServiceLink{{ID: "x", Title: "x", Text: "https://example.com/a\x7f"}},
		},
		{
			"non-ascii is text",
			map[string]string{"cetacean.link.x": "https://example.com/ä"},
			[]ServiceLink{{ID: "x", Title: "x", Text: "https://example.com/ä"}},
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

func TestHTTPURLFollowsRFC3986(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"https://example.com", true},
		{"http://example.com/", true},
		{"HTTPS://Example.COM/a/b", true},
		{"https://example.com/a%20b?q=1&r=a/b?c#top/x?y", true},
		{"https://user:pass@example.com:8443/p", true},
		{"https://[2001:db8::1]:8080/p", true},
		{"https://[::1]/", true},
		{"https://example.com:/p", true},
		{"https://example.com/a:b@c!$&'()*+,;=-._~", true},

		{"ftp://example.com", false},
		{"javascript:alert(1)", false},
		{"https:example.com", false},
		{"https://", false},
		{"https://:8080/path", false},
		{"https://example.com/a b", false},
		{"https://example.com/a\x7f", false},
		{"https://example.com/ä", false},
		{"https://example.com/a[b]", false},
		{"https://example.com/#a#b", false},
		{"https://example.com/?q=%zz", false},
		{"https://example.com/%2", false},
		{"https://example.com/%", false},
		{"https://ex[ample.com/", false},
		{"https://[example.com]/", false},
		{"https://[192.0.2.1]/", false},
		{"https://[fe80::1%25en0]/", false},
		{"https://[::1/", false},
		{"https://[::1]x/", false},
		{"https://example.com:80a/", false},
		{"https://example.com:80:81/", false},
		{"https://us er@example.com/", false},
		{`https://example.com/"q"`, false},
		{"https://example.com/a\\b", false},
		{"https://example.com/{x}", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := isHTTPURL(tt.value); got != tt.want {
				t.Errorf("isHTTPURL(%q) = %v, want %v", tt.value, got, tt.want)
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
