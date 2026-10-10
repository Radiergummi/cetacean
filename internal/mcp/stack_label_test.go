package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

// A stack-scoped write grant must not reach another tenant's stack by
// relabelling a resource into it, through any tool that takes labels.
func TestStackLabelNeedsWriteOnTheDestinationStack(t *testing.T) {
	const ns = cluster.StackNamespaceLabel

	calls := []struct {
		tool string
		args func(target string) map[string]any
	}{
		{"update_service", func(target string) map[string]any {
			return map[string]any{
				"id": "svc1", "section": sectionLabels,
				"value": map[string]any{ns: target},
			}
		}},
		{"create_config", func(target string) map[string]any {
			return map[string]any{
				"name": "acme_cfg", "data": "x", "labels": map[string]any{ns: target},
			}
		}},
		{"create_secret", func(target string) map[string]any {
			return map[string]any{
				"name": "acme_sec", "data": "x", "labels": map[string]any{ns: target},
			}
		}},
	}

	for _, call := range calls {
		for _, tt := range []struct {
			target string
			denied bool
		}{{"globex", true}, {"acme-staging", false}} {
			t.Run(call.tool+" into "+tt.target, func(t *testing.T) {
				c := cache.New(nil)
				c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{
						Name: "acme_web", Labels: map[string]string{ns: "acme"},
					},
				}})

				e := acl.NewEvaluator()
				e.SetResolver(c)
				e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
					Resources: []string{
						"stack:acme", "stack:acme-staging", "config:acme_*", "secret:acme_*",
					},
					Audience:    []string{"*"},
					Permissions: []string{"read", "write"},
				}}})

				written := false
				wc := &fakeWriteClient{
					simulatedLabels: map[string]string{ns: "acme"},
					updateServiceLabelsFn: func(
						_ context.Context, id string, _ map[string]string,
					) (swarm.Service, error) {
						written = true
						return swarm.Service{ID: id}, nil
					},
					createConfigFn: func(context.Context, swarm.ConfigSpec) (string, error) {
						written = true
						return "cfg1", nil
					},
					createSecretFn: func(context.Context, swarm.SecretSpec) (string, error) {
						written = true
						return "sec1", nil
					},
				}

				srv := newToolTestServer(t, c, wc, config.OpsImpactful,
					func(o *Options) { o.ACL = e })
				td, ok := srv.findTool(call.tool)
				if !ok {
					t.Fatalf("%s not registered", call.tool)
				}

				ctx := auth.ContextWithIdentity(
					context.Background(), &auth.Identity{Subject: "acme"},
				)
				_, err := td.handler(ctx, newCallToolRequest(call.tool, call.args(tt.target)))

				if !tt.denied {
					if err != nil {
						t.Fatalf("handler: %v", err)
					}
					return
				}

				if _, ok := errors.AsType[*cluster.StackLabelDeniedError](err); !ok {
					t.Fatalf("err = %v, want StackLabelDeniedError", err)
				}
				if written {
					t.Fatal("the write reached Docker despite the denial")
				}
			})
		}
	}
}
