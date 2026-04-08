package acl

import (
	"fmt"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

// --- Can() benchmarks ---

func BenchmarkCan_NoLabels(b *testing.B) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:webapp-*"},
			Audience:    []string{"group:ops"},
			Permissions: []string{"write"},
		},
	}})
	id := &auth.Identity{Subject: "alice", Groups: []string{"ops"}}

	b.ResetTimer()
	for range b.N {
		e.Can(id, "read", "service:webapp-api")
	}
}

func BenchmarkCan_LabelsMatch(b *testing.B) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {
				"cetacean.acl.read":  "group:dev,group:ops",
				"cetacean.acl.write": "group:ops",
			},
		},
	})
	id := &auth.Identity{Subject: "alice", Groups: []string{"ops"}}

	b.ResetTimer()
	for range b.N {
		e.Can(id, "write", "service:webapp")
	}
}

func BenchmarkCan_LabelsNoMatch_FallThrough(b *testing.B) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"group:dev"},
			Permissions: []string{"read"},
		},
	}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:ops"},
		},
	})
	id := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}

	b.ResetTimer()
	for range b.N {
		e.Can(id, "read", "service:webapp")
	}
}

func BenchmarkCan_LabelsNoLabelsOnResource(b *testing.B) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"group:ops"},
			Permissions: []string{"read"},
		},
	}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{},
	})
	id := &auth.Identity{Subject: "alice", Groups: []string{"ops"}}

	b.ResetTimer()
	for range b.N {
		e.Can(id, "read", "service:webapp")
	}
}

func BenchmarkCan_TaskInheritance(b *testing.B) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		services: map[string]string{"task-1": "webapp"},
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:dev"},
		},
	})
	id := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}

	b.ResetTimer()
	for range b.N {
		e.Can(id, "read", "task:task-1")
	}
}

// --- Filter() benchmarks ---

type benchSvc struct{ name string }

func makeBenchItems(n int, labeledFraction float64) ([]benchSvc, *stubResolver) {
	items := make([]benchSvc, n)
	labels := make(map[string]map[string]string)
	labeledCount := int(float64(n) * labeledFraction)

	for i := range n {
		name := fmt.Sprintf("svc-%d", i)
		items[i] = benchSvc{name: name}
		if i < labeledCount {
			labels["service:"+name] = map[string]string{
				"cetacean.acl.read": "group:dev,group:ops",
			}
		}
	}

	return items, &stubResolver{labels: labels}
}

func BenchmarkFilter_WithLabels(b *testing.B) {
	for _, n := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			items, resolver := makeBenchItems(n, 0.5)
			e := NewEvaluator()
			e.SetLabelsEnabled(true)
			e.SetPolicy(&Policy{Grants: []Grant{
				{
					Resources:   []string{"service:*"},
					Audience:    []string{"group:dev"},
					Permissions: []string{"read"},
				},
			}})
			e.SetResolver(resolver)
			id := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
			resourceFunc := func(s benchSvc) string { return "service:" + s.name }

			b.ResetTimer()
			for range b.N {
				Filter(e, id, "read", items, resourceFunc)
			}
		})
	}
}

func BenchmarkFilter_NoLabels(b *testing.B) {
	for _, n := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			items := make([]benchSvc, n)
			for i := range n {
				items[i] = benchSvc{name: fmt.Sprintf("svc-%d", i)}
			}
			e := NewEvaluator()
			e.SetPolicy(&Policy{Grants: []Grant{
				{
					Resources:   []string{"service:*"},
					Audience:    []string{"group:dev"},
					Permissions: []string{"read"},
				},
			}})
			id := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
			resourceFunc := func(s benchSvc) string { return "service:" + s.name }

			b.ResetTimer()
			for range b.N {
				Filter(e, id, "read", items, resourceFunc)
			}
		})
	}
}

// --- ParseACLLabels benchmark ---

func BenchmarkParseACLLabels(b *testing.B) {
	labels := map[string]string{
		"cetacean.acl.read":          "group:dev,group:ops,user:alice@example.com",
		"cetacean.acl.write":         "group:ops",
		"com.docker.stack.namespace": "mystack",
		"traefik.enable":             "true",
	}

	b.ResetTimer()
	for range b.N {
		ParseACLLabels(labels)
	}
}

func BenchmarkParseAudienceList(b *testing.B) {
	b.Run("short", func(b *testing.B) {
		for range b.N {
			ParseAudienceList("group:ops")
		}
	})
	b.Run("typical", func(b *testing.B) {
		for range b.N {
			ParseAudienceList("group:dev,group:ops,user:alice@example.com")
		}
	})
	b.Run("long", func(b *testing.B) {
		// 20 audience entries
		entries := make([]string, 20)
		for i := range entries {
			entries[i] = fmt.Sprintf("group:team-%d", i)
		}
		value := strings.Join(entries, ",")
		b.ResetTimer()
		for range b.N {
			ParseAudienceList(value)
		}
	})
}

// --- hasACLLabels benchmark ---

func BenchmarkHasACLLabels(b *testing.B) {
	b.Run("present", func(b *testing.B) {
		labels := map[string]string{
			"cetacean.acl.read":          "group:dev",
			"com.docker.stack.namespace": "mystack",
		}
		b.ResetTimer()
		for range b.N {
			hasACLLabels(labels)
		}
	})
	b.Run("absent", func(b *testing.B) {
		labels := map[string]string{
			"com.docker.stack.namespace": "mystack",
			"traefik.enable":             "true",
		}
		b.ResetTimer()
		for range b.N {
			hasACLLabels(labels)
		}
	})
}
