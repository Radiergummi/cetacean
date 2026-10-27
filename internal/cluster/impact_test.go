package cluster

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestReferenceRemovalImpact_CountsWhatItCannotName(t *testing.T) {
	users := []cache.ServiceRef{
		{ID: "1", Name: "shop_web"},
		{ID: "2", Name: "billing_api"},
		{ID: "3", Name: "billing_worker"},
		{ID: "4", Name: "shop_admin"},
	}
	readable := func(resource string) bool { return strings.HasPrefix(resource, "service:shop_") }

	impact := ReferenceRemovalImpact("secret", "db_password", users, readable)

	if got := strings.Join(impact.Dependents, ","); got != "shop_admin,shop_web" {
		t.Errorf("dependents = %q", got)
	}
	if impact.HiddenDependents != 2 {
		t.Errorf("hidden = %d, want 2", impact.HiddenDependents)
	}

	message := impact.Message()
	if strings.Contains(message, "billing") {
		t.Errorf("the message names a service the caller can't read:\n%s", message)
	}
	for _, want := range []string{
		"Remove secret db_password.",
		"Used by shop_admin, shop_web and 2 other services you can't see.",
		"can't be recovered",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("message lacks %q:\n%s", want, message)
		}
	}
}

func TestReferenceRemovalImpact_AllHidden(t *testing.T) {
	impact := ReferenceRemovalImpact("config", "app",
		[]cache.ServiceRef{{Name: "a"}}, func(string) bool { return false })

	if !strings.Contains(impact.Message(), "Used by 1 service you can't see.") {
		t.Errorf("message = %q", impact.Message())
	}
}

// Docker refuses only while a current spec uses a secret; a previous spec
// alone does not stop it, so that is a broken rollback rather than a refusal.
func TestReferenceRemovalImpact_RollbackOnly(t *testing.T) {
	users := []cache.ServiceRef{
		{Name: "shop_web", Rollback: true},
		{Name: "billing_api", Rollback: true},
	}
	readable := func(resource string) bool { return strings.HasPrefix(resource, "service:shop_") }

	message := ReferenceRemovalImpact("secret", "db_v1", users, readable).Message()

	if strings.Contains(message, "refuses") || strings.Contains(message, "Used by") {
		t.Errorf("a rollback-only secret reads as refused:\n%s", message)
	}
	if want := "Removing it breaks a rollback of shop_web and 1 other service you can't see."; !strings.Contains(
		message,
		want,
	) {
		t.Errorf("message lacks %q:\n%s", want, message)
	}

	mixed := ReferenceRemovalImpact("secret", "db_v1",
		append(users, cache.ServiceRef{Name: "shop_api"}), readable).Message()
	if !strings.Contains(mixed, "refuses") || !strings.Contains(mixed, "Used by shop_api.") {
		t.Errorf("a current user is not reported as one:\n%s", mixed)
	}
}

func TestReferenceRemovalImpact_Unused(t *testing.T) {
	impact := ReferenceRemovalImpact("network", "web", nil, func(string) bool { return true })

	if len(impact.Effects) != 0 || strings.Contains(impact.Message(), "Used by") {
		t.Errorf("an unused network reported users: %q", impact.Message())
	}
}

func manager(id string, reachable bool) swarm.Node {
	reachability := swarm.ReachabilityUnreachable
	if reachable {
		reachability = swarm.ReachabilityReachable
	}

	return swarm.Node{
		ID:            id,
		Spec:          swarm.NodeSpec{Role: swarm.NodeRoleManager},
		Description:   swarm.NodeDescription{Hostname: id},
		ManagerStatus: &swarm.ManagerStatus{Reachability: reachability},
	}
}

func worker(id string) swarm.Node {
	return swarm.Node{
		ID:          id,
		Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker},
		Description: swarm.NodeDescription{Hostname: id},
	}
}

func TestRoleImpact(t *testing.T) {
	cases := map[string]struct {
		nodes []swarm.Node
		node  int
		role  swarm.NodeRole
		want  []string
	}{
		"demote one of three": {
			nodes: []swarm.Node{manager("m1", true), manager("m2", true), manager("m3", true)},
			node:  2,
			role:  swarm.NodeRoleWorker,
			want: []string{
				"Demote node m3 to worker.",
				"Managers: 3 before, 2 after.",
				"Quorum survives: 2 of 2 managers reachable, 2 needed; it then tolerates 0 manager failures.",
			},
		},
		"demote with one already unreachable": {
			nodes: []swarm.Node{manager("m1", true), manager("m2", false), manager("m3", true)},
			node:  2,
			role:  swarm.NodeRoleWorker,
			want: []string{
				"Quorum is lost: 1 of 2 managers reachable, 2 needed.",
			},
		},
		"demote the last manager": {
			nodes: []swarm.Node{manager("m1", true), worker("w1")},
			node:  0,
			role:  swarm.NodeRoleWorker,
			want:  []string{"Leaves the swarm with no manager."},
		},
		"promote a third": {
			nodes: []swarm.Node{manager("m1", true), manager("m2", true), worker("w1")},
			node:  2,
			role:  swarm.NodeRoleManager,
			want: []string{
				"Promote node w1 to manager.",
				"Managers: 2 before, 3 after.",
				"it then tolerates 1 manager failure.",
			},
		},
		"promote a fourth beside an unreachable manager": {
			nodes: []swarm.Node{
				manager("m1", true), manager("m2", true), manager("m3", false), worker("w1"),
			},
			node: 3,
			role: swarm.NodeRoleManager,
			want: []string{
				"Quorum survives: 3 of 4 managers reachable, 3 needed; it then tolerates 0 manager failures.",
			},
		},
		"no change": {
			nodes: []swarm.Node{manager("m1", true)},
			node:  0,
			role:  swarm.NodeRoleManager,
			want:  []string{"already a manager; nothing changes"},
		},
	}

	for name, tc := range cases {
		message := RoleImpact(tc.nodes[tc.node], tc.nodes, tc.role).Message()
		for _, want := range tc.want {
			if !strings.Contains(message, want) {
				t.Errorf("%s: message lacks %q:\n%s", name, want, message)
			}
		}
	}
}

func TestDrainImpact_SummarisesTheGraph(t *testing.T) {
	graph := TopologyGraph{
		Nodes: []TopologyNode{
			{ID: "n2", Label: "worker-2", Type: "node"},
			{ID: "s1", Label: "web", Type: "service", State: drainStateMovable},
			{ID: "s2", Label: "agent", Type: "service", State: drainStateGlobal},
			{ID: "s3", Label: "db", Type: "service", State: drainStateStranded},
		},
		Note: "assessed against the 2 node(s) you can read",
	}

	message := DrainImpact(worker("worker-1"), graph, 3).Message()

	for _, want := range []string{
		"Drain node worker-1.",
		"Moves the tasks of 1 service to other nodes.",
		"Stops the tasks of 1 global service here: agent.",
		"Leaves 1 service with nowhere to run: db.",
		"This was assessed against the 2 node(s) you can read.",
		"Runs tasks of web, agent, db and 3 other services you can't see.",
		"doesn't move tasks back",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("message lacks %q:\n%s", want, message)
		}
	}
}

func TestTaskRemovalImpact(t *testing.T) {
	if got := TaskRemovalImpact("web.1", "web").Message(); !strings.Contains(got,
		"replacement for service web") {
		t.Errorf("message = %q", got)
	}

	if got := TaskRemovalImpact("abc", "").Message(); strings.Contains(got, "service ") &&
		!strings.Contains(got, "if the task's service") {
		t.Errorf("message = %q", got)
	}
}

func TestServiceRemovalImpact(t *testing.T) {
	svc := swarm.Service{Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}}}

	message := ServiceRemovalImpact(svc, 1).Message()
	for _, want := range []string{"Remove service web.", "Stops its 1 running task.", "can't be undone"} {
		if !strings.Contains(message, want) {
			t.Errorf("message lacks %q:\n%s", want, message)
		}
	}
}
