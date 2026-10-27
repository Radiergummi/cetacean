package cluster

import (
	"reflect"
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

func TestStackLinks(t *testing.T) {
	service := func(labels map[string]string) swarm.Service {
		return swarm.Service{
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Labels: labels}},
		}
	}

	got := StackLinks([]swarm.Service{
		service(map[string]string{
			"cetacean.link.repo":    "https://git.example.com/app",
			"cetacean.link.grafana": "https://grafana.example.com/api",
		}),
		service(map[string]string{
			"cetacean.link.repo":    "https://git.example.com/app",
			"cetacean.link.grafana": "https://grafana.example.com/worker",
		}),
		service(map[string]string{
			"cetacean.link.source":       "https://git.example.com/app",
			"cetacean.link.source.title": "repo",
		}),
		service(nil),
	})

	want := []ServiceLink{
		{ID: "grafana", Title: "grafana", Href: "https://grafana.example.com/api"},
		{ID: "grafana", Title: "grafana", Href: "https://grafana.example.com/worker"},
		{ID: "repo", Title: "repo", Href: "https://git.example.com/app"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
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
