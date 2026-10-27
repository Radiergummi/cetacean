package integrations

import "testing"

func TestDetectCaddy(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name: "site with reverse proxy",
			labels: map[string]string{
				"caddy":               "example.com",
				"caddy.reverse_proxy": "{{upstreams 8080}}",
			},
			want: "example.com {\n\treverse_proxy {{upstreams 8080}}\n}\n",
		},
		{
			name: "subdirectives group under their directive",
			labels: map[string]string{
				"caddy":                     "example.com",
				"caddy.directive":           "argA",
				"caddy.directive.subdirB":   "valueB1 valueB2",
				"caddy.directive.subdirA":   "valueA",
				"caddy.encode":              "",
				"caddy.@match.path":         "/api/*",
				"caddy.reverse_proxy":       "@match localhost:6001",
				"com.docker.stack.platform": "ignored",
			},
			want: "example.com {\n" +
				"\t@match {\n\t\tpath /api/*\n\t}\n" +
				"\tdirective argA {\n\t\tsubdirA valueA\n\t\tsubdirB valueB1 valueB2\n\t}\n" +
				"\tencode\n" +
				"\treverse_proxy @match localhost:6001\n" +
				"}\n",
		},
		{
			name: "suffix isolates and prefix orders",
			labels: map[string]string{
				"caddy":           "example.com",
				"caddy.route_1.b": "value",
				"caddy.route_0.a": "value",
				"caddy.2_aaa":     "second",
				"caddy.1_bbb":     "first",
			},
			want: "example.com {\n" +
				"\tbbb first\n\taaa second\n" +
				"\troute {\n\t\ta value\n\t}\n" +
				"\troute {\n\t\tb value\n\t}\n" +
				"}\n",
		},
		{
			name: "global options, snippets, then sites",
			labels: map[string]string{
				"caddy_1":        "site-a.com",
				"caddy_1.import": "snippet",
				"caddy_0":        "(snippet)",
				"caddy_0.tls":    "internal",
				"caddy_2.email":  "you@example.com",
			},
			want: "{\n\temail you@example.com\n}\n" +
				"(snippet) {\n\ttls internal\n}\n" +
				"site-a.com {\n\timport snippet\n}\n",
		},
		{
			name: "escaped dots stay in the name",
			labels: map[string]string{
				"caddy":                 "example.com",
				`caddy.header.X\.Trace`: "on",
			},
			want: "example.com {\n\theader {\n\t\tX.Trace on\n\t}\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectCaddy(tt.labels)
			if got == nil {
				t.Fatal("expected non-nil result")
			}

			if got.Name != "caddy" || !got.Enabled {
				t.Errorf("Name = %q, Enabled = %v", got.Name, got.Enabled)
			}

			if got.Caddyfile != tt.want {
				t.Errorf("Caddyfile =\n%s\nwant\n%s", got.Caddyfile, tt.want)
			}
		})
	}
}

func TestDetectCaddy_IgnoresLookalikes(t *testing.T) {
	labels := map[string]string{
		"caddy_ingress_network": "proxy",
		"caddyfile_path":        "/etc/caddy",
		"caddy_x.foo":           "bar",
	}

	if got := detectCaddy(labels); got != nil {
		t.Errorf("expected nil result, got %+v", got)
	}
}
