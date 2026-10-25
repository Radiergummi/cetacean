package imagecheck

import (
	"reflect"
	"testing"
)

func TestParseChallenges(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   []challenge
	}{
		{
			name: "docker hub",
			values: []string{
				`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`,
			},
			want: []challenge{{scheme: "bearer", params: map[string]string{
				"realm":   "https://auth.docker.io/token",
				"service": "registry.docker.io",
				"scope":   "repository:library/nginx:pull",
			}}},
		},
		{
			name:   "scheme and parameter names are case-insensitive",
			values: []string{`BEARER Realm="https://r.test/t", SERVICE=reg`},
			want: []challenge{{scheme: "bearer", params: map[string]string{
				"realm": "https://r.test/t", "service": "reg",
			}}},
		},
		{
			name:   "two challenges in one field",
			values: []string{`Basic realm="x", Bearer realm="https://r.test/t", scope="a:b"`},
			want: []challenge{
				{scheme: "basic", params: map[string]string{"realm": "x"}},
				{
					scheme: "bearer",
					params: map[string]string{"realm": "https://r.test/t", "scope": "a:b"},
				},
			},
		},
		{
			name:   "quoted-pair and whitespace around the equals sign",
			values: []string{`Bearer realm = "https://r.test/\"t\"", error="a\\b"`},
			want: []challenge{{scheme: "bearer", params: map[string]string{
				"realm": `https://r.test/"t"`, "error": `a\b`,
			}}},
		},
		{
			name:   "token68 is skipped",
			values: []string{`Negotiate abc==, Bearer realm="https://r.test/t"`},
			want: []challenge{
				{scheme: "negotiate", params: map[string]string{}},
				{scheme: "bearer", params: map[string]string{"realm": "https://r.test/t"}},
			},
		},
		{
			name:   "one challenge per field",
			values: []string{`Basic realm="x"`, `Bearer realm="https://r.test/t"`},
			want: []challenge{
				{scheme: "basic", params: map[string]string{"realm": "x"}},
				{scheme: "bearer", params: map[string]string{"realm": "https://r.test/t"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseChallenges(tc.values); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}
