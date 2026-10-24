package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// listPatchRoutes are the merge-patch endpoints whose one member replaces a list.
var listPatchRoutes = []struct {
	member  string
	handler func(*Handlers) http.HandlerFunc
	// present is what the cached service holds, so a no-op answer shows it.
	present string
}{
	{
		"ports",
		func(h *Handlers) http.HandlerFunc { return h.HandlePatchServicePorts },
		`"TargetPort":80`,
	},
	{
		"configs",
		func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceConfigs },
		`"cfg1"`,
	},
	{
		"secrets",
		func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceSecrets },
		`"sec1"`,
	},
	{
		"networks",
		func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceNetworks },
		`"net1"`,
	},
	{"mounts", func(h *Handlers) http.HandlerFunc { return h.HandlePatchServiceMounts }, `"/data"`},
}

func serviceWithEveryList() swarm.Service {
	return swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Configs: []*swarm.ConfigReference{{ConfigID: "cfg1", ConfigName: "c"}},
					Secrets: []*swarm.SecretReference{{SecretID: "sec1", SecretName: "s"}},
					Mounts:  []mount.Mount{{Type: mount.TypeTmpfs, Target: "/data"}},
				},
				Networks: []swarm.NetworkAttachmentConfig{{Target: "net1"}},
			},
			EndpointSpec: &swarm.EndpointSpec{
				Ports: []swarm.PortConfig{{Protocol: "tcp", TargetPort: 80}},
			},
		},
	}
}

func patchList(
	h *Handlers,
	handler http.HandlerFunc,
	member, body string,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/services/svc1/"+member, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	handler(w, req)

	return w
}

// RFC 7396: a member absent from the patch leaves the target's member as it is.
func TestAListMergePatchWithoutItsMemberChangesNothing(t *testing.T) {
	for _, route := range listPatchRoutes {
		for _, body := range []string{`{}`} {
			t.Run(route.member+" "+body, func(t *testing.T) {
				c := cache.New(nil)
				c.SetService(serviceWithEveryList())

				// Every write of the zero mock fails, so reaching one is a 500.
				h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
				w := patchList(h, route.handler(h), route.member, body)

				if w.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
				}
				if !strings.Contains(w.Body.String(), route.present) {
					t.Errorf(
						"body does not show the unchanged list (%s): %s",
						route.present,
						w.Body.String(),
					)
				}
			})
		}
	}
}

func TestAListMergePatchOfNullClearsTheList(t *testing.T) {
	cleared := map[string]bool{}
	mock := &mockWriteClient{}
	mock.updateServicePortsFn = func(_ context.Context, _ string, p []swarm.PortConfig) (swarm.Service, error) {
		cleared["ports"] = len(p) == 0
		return swarm.Service{ID: "svc1"}, nil
	}
	mock.updateServiceConfigsFn = func(_ context.Context, _ string, c []*swarm.ConfigReference) (swarm.Service, error) {
		cleared["configs"] = len(c) == 0
		return swarm.Service{ID: "svc1"}, nil
	}
	mock.updateServiceSecretsFn = func(_ context.Context, _ string, s []*swarm.SecretReference) (swarm.Service, error) {
		cleared["secrets"] = len(s) == 0
		return swarm.Service{ID: "svc1"}, nil
	}
	mock.updateServiceNetworksFn = func(
		_ context.Context,
		_ string,
		n []swarm.NetworkAttachmentConfig,
	) (swarm.Service, error) {
		cleared["networks"] = len(n) == 0
		return swarm.Service{ID: "svc1"}, nil
	}
	mock.updateServiceMountsFn = func(_ context.Context, _ string, m []mount.Mount) (swarm.Service, error) {
		cleared["mounts"] = len(m) == 0
		return swarm.Service{ID: "svc1"}, nil
	}

	for _, route := range listPatchRoutes {
		for _, value := range []string{"null", "[]"} {
			t.Run(route.member+" "+value, func(t *testing.T) {
				clear(cleared)

				c := cache.New(nil)
				c.SetService(serviceWithEveryList())
				h := newTestHandlers(t, withCache(c), withWriteClient(mock))
				w := patchList(h, route.handler(h), route.member, `{"`+route.member+`":`+value+`}`)

				if w.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
				}
				if !cleared[route.member] {
					t.Errorf("%s was not written as an empty list", route.member)
				}
			})
		}
	}
}

func TestAListMergePatchThatIsNotAnObjectIsRefused(t *testing.T) {
	for _, route := range listPatchRoutes {
		for _, body := range []string{`null`, `[]`, `"ports"`} {
			t.Run(route.member+" "+body, func(t *testing.T) {
				c := cache.New(nil)
				c.SetService(serviceWithEveryList())
				h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
				w := patchList(h, route.handler(h), route.member, body)

				if w.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400: %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

// Member names are compared exactly (RFC 7396), so a misspelt or differently
// cased one would otherwise change nothing and still answer 200.
func TestAListMergePatchNamingAnotherMemberIsRefused(t *testing.T) {
	for _, route := range listPatchRoutes {
		upper := strings.ToUpper(route.member[:1]) + route.member[1:]
		for _, body := range []string{
			`{"` + upper + `":[]}`,
			`{"` + route.member + `s":[]}`,
			`{"` + route.member + `":[],"extra":1}`,
		} {
			t.Run(route.member+" "+body, func(t *testing.T) {
				c := cache.New(nil)
				c.SetService(serviceWithEveryList())
				h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))
				w := patchList(h, route.handler(h), route.member, body)

				if w.Code != http.StatusBadRequest ||
					!strings.Contains(w.Body.String(), "API008") {
					t.Errorf("status = %d, want 400 API008: %s", w.Code, w.Body.String())
				}
			})
		}
	}
}
