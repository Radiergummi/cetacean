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

func inStack(stack string) map[string]string {
	return map[string]string{"com.docker.stack.namespace": stack}
}

func TestGroup(t *testing.T) {
	tests := []struct {
		name  string
		items []Item
		want  map[string]Membership
	}{
		{
			"docker-stack-deploy sha1 suffixes",
			[]Item{
				item("a", "app_env_0a1b2c3d4e5f", 1, nil),
				item("b", "app_env_9f8e7d6c5b4a", 2, nil),
			},
			map[string]Membership{
				"a": {Of: "app_env", Inferred: true},
				"b": {Of: "app_env", Inferred: true, Newest: true},
			},
		},
		{
			"SwarmCD md5 suffixes",
			[]Item{
				item("a", "web-nginx-1a2b3c4d", 1, inStack("web")),
				item("b", "web-nginx-5e6f7a8b", 2, inStack("web")),
			},
			map[string]Membership{
				"a": {Of: "web-nginx", Inferred: true},
				"b": {Of: "web-nginx", Inferred: true, Newest: true},
			},
		},
		{
			"uppercase hex",
			[]Item{item("a", "key_DEADBEEF", 1, nil), item("b", "key_CAFEBABE", 2, nil)},
			map[string]Membership{
				"a": {Of: "key", Inferred: true},
				"b": {Of: "key", Inferred: true, Newest: true},
			},
		},
		{
			"numbered versions join the unsuffixed original",
			[]Item{
				item("a", "db_password", 1, nil),
				item("b", "db_password_v2", 2, nil),
				item("c", "db_password_V3", 3, nil),
			},
			map[string]Membership{
				"a": {Of: "db_password", Inferred: true},
				"b": {Of: "db_password", Inferred: true},
				"c": {Of: "db_password", Inferred: true, Newest: true},
			},
		},
		{
			"bare numeric suffixes are not versions",
			[]Item{item("a", "worker_1", 1, nil), item("b", "worker_2", 2, nil)},
			map[string]Membership{},
		},
		{
			"short hex suffixes are not versions",
			[]Item{item("a", "app_abc123", 1, nil), item("b", "app_def456", 2, nil)},
			map[string]Membership{},
		},
		{
			"different stacks are not grouped",
			[]Item{
				item("a", "conf_v1", 1, inStack("one")),
				item("b", "conf_v2", 2, inStack("two")),
			},
			map[string]Membership{},
		},
		{
			"a stack and no stack are not grouped",
			[]Item{item("a", "conf_v1", 1, inStack("one")), item("b", "conf_v2", 2, nil)},
			map[string]Membership{},
		},
		{
			"a lone version has no group",
			[]Item{item("a", "app_0a1b2c3d4e5f", 1, nil), item("b", "other", 2, nil)},
			map[string]Membership{},
		},
		{
			"the label overrides the name",
			[]Item{
				item("a", "tls-old", 1, map[string]string{Label: "tls"}),
				item("b", "tls-new", 2, map[string]string{Label: "tls"}),
				// Would join a and b by name if it were not for its label.
				item("c", "tls_v3", 3, map[string]string{Label: "other"}),
			},
			map[string]Membership{
				"a": {Of: "tls"},
				"b": {Of: "tls", Newest: true},
			},
		},
		{
			"a labelled version joins unlabelled ones by name, inferred",
			[]Item{
				item("a", "api_v1", 1, nil),
				item("b", "api_rotated", 2, map[string]string{Label: "api"}),
			},
			map[string]Membership{
				"a": {Of: "api", Inferred: true},
				"b": {Of: "api", Inferred: true, Newest: true},
			},
		},
		{
			"a creation tie goes to the greater ID",
			[]Item{item("x", "s_v1", 1, nil), item("y", "s_v2", 1, nil)},
			map[string]Membership{
				"x": {Of: "s", Inferred: true},
				"y": {Of: "s", Inferred: true, Newest: true},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Group(test.items)
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

	secrets := Secrets([]swarm.Secret{secret("a", "k_v1", 2), secret("b", "k_v2", 1)})
	if !secrets["a"].Newest || secrets["b"].Newest {
		t.Errorf("Secrets() = %v, want a newest", secrets)
	}

	configs := Configs([]swarm.Config{config("a", "c_v1", 1), config("b", "c_v2", 2)})
	if configs["a"].Newest || !configs["b"].Newest {
		t.Errorf("Configs() = %v, want b newest", configs)
	}
}
