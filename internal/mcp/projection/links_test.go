package projection

import (
	"reflect"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

func TestDigestsCarryLinks(t *testing.T) {
	svc := swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
		Name:   "web",
		Labels: map[string]string{"cetacean.link.repo": "https://git.example.com/web"},
	}}}
	want := []cluster.ServiceLink{{ID: "repo", Title: "repo", Href: "https://git.example.com/web"}}

	if got := ServiceDetails(svc)["links"]; !reflect.DeepEqual(got, want) {
		t.Errorf("service links = %+v, want %+v", got, want)
	}

	want[0].Services = []string{"web"}

	stack := StackDigest(
		cache.StackDetail{Name: "demo", Services: []swarm.Service{svc}},
		nil,
		nil,
		nil,
	)
	if got := stack.Details["links"]; !reflect.DeepEqual(got, want) {
		t.Errorf("stack links = %+v, want %+v", got, want)
	}
}

func TestDigestsOmitLinksWhenNone(t *testing.T) {
	svc := swarm.Service{ID: "svc1"}

	if _, present := ServiceDetails(svc)["links"]; present {
		t.Error("service links present with no link labels")
	}

	stack := StackDigest(
		cache.StackDetail{Name: "demo", Services: []swarm.Service{svc}},
		nil,
		nil,
		nil,
	)
	if _, present := stack.Details["links"]; present {
		t.Error("stack links present with no link labels")
	}
}
