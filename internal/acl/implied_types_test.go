package acl

import (
	"slices"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

// TestImpliedStackTypesMatchTheResolver closes the half TestTypeGrantsAgreesWithCan
// cannot see. That test drives a hand-written stubResolver, so adding a case to
// cache.StackOf's switch — the authority on what a stack contains — leaves
// impliedTypes stale and both tests still green, reshipping for the new type
// exactly the bug this projection exists to fix.
//
// This one drives the real Cache. Every type that can hold a stack namespace
// label gets a resource carrying one, so a type reported as not reaching a
// stack is a type StackOf declines to place, not one missing from the fixture.
func TestImpliedStackTypesMatchTheResolver(t *testing.T) {
	const (
		memberName = "member"
		stackName  = "web"
	)

	labels := map[string]string{"com.docker.stack.namespace": stackName}
	annotations := swarm.Annotations{Name: memberName, Labels: labels}

	resolver := cache.New(nil)
	resolver.SetService(swarm.Service{
		ID:   "service1",
		Spec: swarm.ServiceSpec{Annotations: annotations},
	})
	resolver.SetConfig(swarm.Config{
		ID:   "config1",
		Spec: swarm.ConfigSpec{Annotations: annotations},
	})
	resolver.SetSecret(swarm.Secret{
		ID:   "secret1",
		Spec: swarm.SecretSpec{Annotations: annotations},
	})
	resolver.SetNetwork(network.Summary{ID: "network1", Name: memberName, Labels: labels})
	resolver.SetVolume(volume.Volume{Name: memberName, Labels: labels})
	resolver.SetNode(swarm.Node{
		ID:          "node1",
		Spec:        swarm.NodeSpec{Annotations: annotations},
		Description: swarm.NodeDescription{Hostname: memberName},
	})
	resolver.SetTask(swarm.Task{ID: memberName, ServiceID: "service1"})

	// A task belongs to a stack through its parent service, which StackOf does
	// not do in one hop — grantMatchesResource walks task → service → stack
	// itself. So the resolver correctly reports no stack for it while a stack
	// grant still reaches it.
	const twoHop = "task"

	for resourceType := range validResourceTypes {
		reachesAStack := resolver.StackOf(resourceType, memberName) != ""
		implied := slices.Contains(impliedTypes["stack"], resourceType)

		if resourceType == twoHop {
			if !implied {
				t.Errorf("a stack grant reaches %q through its parent service, "+
					"but impliedTypes does not say so", resourceType)
			}

			continue
		}

		if reachesAStack && !implied {
			t.Errorf("cache.StackOf places %q in a stack, but impliedTypes[\"stack\"] "+
				"omits it — a stack grant would not reveal it in any listing",
				resourceType)
		}

		if implied && !reachesAStack {
			t.Errorf("impliedTypes[\"stack\"] claims %q, but cache.StackOf never places "+
				"one in a stack — listings would over-report", resourceType)
		}
	}
}
