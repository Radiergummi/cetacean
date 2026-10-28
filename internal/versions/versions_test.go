package versions

import (
	"maps"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func item(id, name string, day int, labels map[string]string) Item {
	return Item{ID: id, Name: name, Labels: labels, Created: epoch.AddDate(0, 0, day)}
}

func labels(stack, of string) map[string]string {
	out := map[string]string{}
	if stack != "" {
		out["com.docker.stack.namespace"] = stack
	}
	if of != "" {
		out[Label] = of
	}

	return out
}

// placed is what a test asserts of a Membership: the group, whether it was
// inferred, and the rank, newest first.
type placed struct {
	of       string
	inferred bool
	rank     int
}

func TestGroup(t *testing.T) {
	tests := []struct {
		name  string
		items []Item
		want  map[string]placed
	}{
		{
			"docker-stack-deploy sha1 suffixes",
			[]Item{
				item("a", "app_env_0a1b2c3d4e5f", 1, nil),
				item("b", "app_env_9f8e7d6c5b4a", 2, nil),
			},
			map[string]placed{"a": {"app_env", true, 1}, "b": {"app_env", true, 0}},
		},
		{
			"SwarmCD md5 suffixes, stack prefix stripped",
			[]Item{
				item("a", "web-nginx-1a2b3c4d", 1, labels("web", "")),
				item("b", "web-nginx-5e6f7a8b", 2, labels("web", "")),
			},
			map[string]placed{"a": {"nginx", true, 1}, "b": {"nginx", true, 0}},
		},
		{
			"uppercase hex",
			[]Item{item("a", "key_DEADBEEF", 1, nil), item("b", "key_CAFEBABE", 2, nil)},
			map[string]placed{"a": {"key", true, 1}, "b": {"key", true, 0}},
		},
		{
			"numbered versions join the unsuffixed original",
			[]Item{
				item("a", "db_password", 1, nil),
				item("b", "db_password_v2", 2, nil),
				item("c", "db_password_V3", 3, nil),
			},
			map[string]placed{
				"a": {"db_password", true, 2},
				"b": {"db_password", true, 1},
				"c": {"db_password", true, 0},
			},
		},
		{
			"bare numeric suffixes are not versions",
			[]Item{item("a", "worker_1", 1, nil), item("b", "worker_2", 2, nil)},
			map[string]placed{},
		},
		{
			"an all-digit suffix is not a hash",
			[]Item{
				item("a", "backup_20240101", 1, nil),
				item("b", "backup_20240102", 2, nil),
				item("c", "tenant-10000001", 3, nil),
				item("d", "tenant-10000002", 4, nil),
			},
			map[string]placed{},
		},
		{
			"a hash with a letter is one, digits around it or not",
			[]Item{item("a", "app_1234567a", 1, nil), item("b", "app_b2345678", 2, nil)},
			map[string]placed{"a": {"app", true, 1}, "b": {"app", true, 0}},
		},
		{
			"short hex suffixes are not versions",
			[]Item{item("a", "app_abc123", 1, nil), item("b", "app_def456", 2, nil)},
			map[string]placed{},
		},
		{
			"different stacks are not grouped",
			[]Item{
				item("a", "conf_v1", 1, labels("one", "")),
				item("b", "conf_v2", 2, labels("two", "")),
			},
			map[string]placed{},
		},
		{
			"a stack and no stack are not grouped",
			[]Item{item("a", "conf_v1", 1, labels("one", "")), item("b", "conf_v2", 2, nil)},
			map[string]placed{},
		},
		{
			"a lone version has no group",
			[]Item{item("a", "app_0a1b2c3d4e5f", 1, nil), item("b", "other", 2, nil)},
			map[string]placed{},
		},
		{
			"the label overrides the name",
			[]Item{
				item("a", "tls-old", 1, labels("", "tls")),
				item("b", "tls-new", 2, labels("", "tls")),
				// Would join a and b by name if it were not for its label.
				item("c", "tls_v3", 3, labels("", "other")),
			},
			map[string]placed{"a": {"tls", false, 1}, "b": {"tls", false, 0}},
		},
		{
			"docker-swarm-deployment-action's name label declares the group",
			[]Item{
				item("a", "shop-db_password-4813494", 1, map[string]string{
					"com.docker.stack.namespace":   "shop",
					"com.matchory.deployment.name": "db_password",
				}),
				item("b", "shop-db_password-9f8e7d6", 2, map[string]string{
					"com.docker.stack.namespace":   "shop",
					"com.matchory.deployment.name": "db_password",
				}),
			},
			map[string]placed{"a": {"db_password", false, 1}, "b": {"db_password", false, 0}},
		},
		{
			"the deployment action's label keeps what looks like a stack prefix",
			[]Item{
				item("a", "shop-cert-1111111", 1, map[string]string{
					"com.docker.stack.namespace":   "shop",
					"com.matchory.deployment.name": "cert",
				}),
				item("b", "shop-shop_cert-2222222", 2, map[string]string{
					"com.docker.stack.namespace":   "shop",
					"com.matchory.deployment.name": "shop_cert",
				}),
			},
			map[string]placed{},
		},
		{
			"cetacean.version-of wins over the deployment action's label",
			[]Item{
				item(
					"a",
					"x-1",
					1,
					map[string]string{Label: "x", "com.matchory.deployment.name": "y"},
				),
				item("b", "x-2", 2, labels("", "x")),
			},
			map[string]placed{"a": {"x", false, 1}, "b": {"x", false, 0}},
		},
		{
			"a labeled replacement joins the stack-prefixed original",
			[]Item{
				item("a", "myapp_db_password", 1, labels("myapp", "")),
				item("b", "myapp_db_password_v2", 2, labels("myapp", "db_password")),
			},
			map[string]placed{"a": {"db_password", true, 1}, "b": {"db_password", true, 0}},
		},
		{
			"a labeled replacement without the stack label stays out of the stack's group",
			[]Item{
				item("a", "myapp_db_password", 1, labels("myapp", "")),
				item("b", "db_password_new", 2, labels("", "db_password")),
			},
			map[string]placed{},
		},
		{
			"a label loses its stack prefix but keeps what looks like a version",
			[]Item{
				item("a", "myapp_db_password", 1, labels("myapp", "")),
				item("b", "rotated", 2, labels("myapp", "myapp_db_password")),
				item("c", "api-one", 3, labels("myapp", "api-v1")),
				item("d", "api-two", 4, labels("myapp", "api-v2")),
			},
			map[string]placed{
				"a": {"db_password", true, 1},
				"b": {"db_password", true, 0},
			},
		},
		{
			"an unstacked name keeps what looks like a prefix",
			[]Item{item("a", "myapp_db_v1", 1, nil), item("b", "db_v2", 2, nil)},
			map[string]placed{},
		},
		{
			"a creation tie goes to the greater ID",
			[]Item{item("x", "s_v1", 1, nil), item("y", "s_v2", 1, nil)},
			map[string]placed{"x": {"s", true, 1}, "y": {"s", true, 0}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]placed{}
			for id, m := range Group(test.items) {
				if m.Count < 2 {
					t.Errorf("%s has Count %d", id, m.Count)
				}
				got[id] = placed{m.Of, m.Inferred, m.Rank}
			}

			if !maps.Equal(got, test.want) {
				t.Errorf("Group() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSecretsAndConfigs(t *testing.T) {
	secret := func(id, name string, day int) swarm.Secret {
		return swarm.Secret{
			ID:        id,
			CreatedAt: epoch.AddDate(0, 0, day),
			Spec:      swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}},
		}
	}
	config := func(id, name string, day int) swarm.Config {
		return swarm.Config{
			ID:        id,
			CreatedAt: epoch.AddDate(0, 0, day),
			Spec:      swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name}},
		}
	}

	secrets := Group(SecretItems([]swarm.Secret{secret("a", "k_v1", 2), secret("b", "k_v2", 1)}))
	if secrets["a"].Rank != 0 || secrets["b"].Rank != 1 {
		t.Errorf("Secrets() = %v, want a newest", secrets)
	}

	configs := Group(ConfigItems([]swarm.Config{config("a", "c_v1", 1), config("b", "c_v2", 2)}))
	if configs["a"].Rank != 1 || configs["b"].Rank != 0 {
		t.Errorf("Configs() = %v, want b newest", configs)
	}
}
