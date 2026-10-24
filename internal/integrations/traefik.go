package integrations

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// TraefikIntegration represents parsed Traefik configuration from Docker labels.
type TraefikIntegration struct {
	Name        string              `json:"name"`
	Enabled     bool                `json:"enabled"`
	Routers     []TraefikRouter     `json:"routers,omitempty"`
	Services    []TraefikService    `json:"services,omitempty"`
	Middlewares []TraefikMiddleware `json:"middlewares,omitempty"`
}

// TraefikRouter represents a parsed Traefik HTTP router.
type TraefikRouter struct {
	Name        string      `json:"name"`
	Rule        string      `json:"rule,omitempty"`
	Entrypoints []string    `json:"entrypoints,omitempty"`
	TLS         *TraefikTLS `json:"tls,omitempty"`
	Middlewares []string    `json:"middlewares,omitempty"`
	Service     string      `json:"service,omitempty"`
	Priority    int         `json:"priority,omitempty"`
}

// TraefikTLS represents TLS configuration for a router.
type TraefikTLS struct {
	CertResolver string             `json:"certResolver,omitempty"`
	Domains      []TraefikTLSDomain `json:"domains,omitempty"`
	Options      string             `json:"options,omitempty"`
}

// TraefikTLSDomain represents a TLS domain with main and SANs.
type TraefikTLSDomain struct {
	Main string   `json:"main"`
	SANs []string `json:"sans,omitempty"`
}

// TraefikService represents a parsed Traefik HTTP service.
type TraefikService struct {
	Name   string `json:"name"`
	Port   int    `json:"port,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

// TraefikMiddleware represents a parsed Traefik HTTP middleware.
type TraefikMiddleware struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Config map[string]string `json:"config,omitempty"`
}

var tlsDomainIndexRegexp = regexp.MustCompile(`^tls\.domains\[(\d+)\]\.(.+)$`)

// maxTLSDomains bounds the index a tls.domains label may name. The index sizes
// a slice on every detail read, and anyone with service write sets it.
const maxTLSDomains = 100

func detectTraefik(labels map[string]string) *TraefikIntegration {
	var (
		found      bool
		routerMap  map[string]*TraefikRouter
		serviceMap map[string]*TraefikService
		mwMap      map[string]*TraefikMiddleware
	)

	// Traefik reads label keys case-insensitively. Names and middleware
	// options keep the case they were written in, for display.
	for k, v := range labels {
		parts := strings.SplitN(k, ".", 5)
		if !strings.EqualFold(parts[0], "traefik") {
			continue
		}

		found = true

		if len(parts) < 5 || !strings.EqualFold(parts[1], "http") {
			continue
		}

		category := strings.ToLower(parts[2])
		name := parts[3]
		field := parts[4]

		switch category {
		case "routers":
			if routerMap == nil {
				routerMap = make(map[string]*TraefikRouter)
			}

			r := getOrCreateRouter(routerMap, name)
			parseRouterField(r, strings.ToLower(field), v)
		case "services":
			if serviceMap == nil {
				serviceMap = make(map[string]*TraefikService)
			}

			s := getOrCreateService(serviceMap, name)
			parseServiceField(s, strings.ToLower(field), v)
		case "middlewares":
			if mwMap == nil {
				mwMap = make(map[string]*TraefikMiddleware)
			}

			m := getOrCreateMiddleware(mwMap, name)
			parseMiddlewareField(m, field, v)
		}
	}

	if !found {
		return nil
	}

	integration := &TraefikIntegration{
		Name:    "traefik",
		Enabled: traefikEnabled(labels),
	}

	integration.Routers = sortedValues(routerMap, func(r TraefikRouter) string { return r.Name })
	integration.Services = sortedValues(serviceMap, func(s TraefikService) string { return s.Name })
	integration.Middlewares = sortedValues(
		mwMap,
		func(m TraefikMiddleware) string { return m.Name },
	)

	return integration
}

// traefikEnabled reads traefik.enable as Traefik does: any key case, and a Go
// boolean. A value that does not parse disables it, as Traefik skips the
// service; an absent one leaves it enabled.
func traefikEnabled(labels map[string]string) bool {
	for k, v := range labels {
		if strings.EqualFold(k, "traefik.enable") {
			on, err := strconv.ParseBool(v)
			return err == nil && on
		}
	}

	return true
}

func getOrCreateRouter(m map[string]*TraefikRouter, name string) *TraefikRouter {
	if r, ok := m[name]; ok {
		return r
	}
	r := &TraefikRouter{Name: name}
	m[name] = r
	return r
}

func getOrCreateService(m map[string]*TraefikService, name string) *TraefikService {
	if s, ok := m[name]; ok {
		return s
	}
	s := &TraefikService{Name: name}
	m[name] = s
	return s
}

func getOrCreateMiddleware(m map[string]*TraefikMiddleware, name string) *TraefikMiddleware {
	if mw, ok := m[name]; ok {
		return mw
	}
	mw := &TraefikMiddleware{
		Name: name,
	}
	m[name] = mw
	return mw
}

func parseRouterField(r *TraefikRouter, field, value string) {
	switch {
	case field == "rule":
		r.Rule = value
	case field == "entrypoints":
		r.Entrypoints = splitComma(value)
	case field == "middlewares":
		r.Middlewares = splitComma(value)
	case field == "service":
		r.Service = value
	case field == "priority":
		if n, err := strconv.Atoi(value); err == nil {
			r.Priority = n
		}
	case field == "tls":
		if on, err := strconv.ParseBool(value); err != nil || on {
			ensureTLS(r)
		}
	case field == "tls.certresolver":
		ensureTLS(r)
		r.TLS.CertResolver = value
	case field == "tls.options":
		ensureTLS(r)
		r.TLS.Options = value
	case strings.HasPrefix(field, "tls.domains["):
		parseTLSDomain(r, field, value)
	}
}

func ensureTLS(r *TraefikRouter) {
	if r.TLS == nil {
		r.TLS = &TraefikTLS{}
	}
}

func parseTLSDomain(r *TraefikRouter, field, value string) {
	matches := tlsDomainIndexRegexp.FindStringSubmatch(field)
	if matches == nil {
		return
	}

	index, err := strconv.Atoi(matches[1])
	if err != nil || index >= maxTLSDomains {
		return
	}

	ensureTLS(r)

	// Grow slice to accommodate index.
	for len(r.TLS.Domains) <= index {
		r.TLS.Domains = append(r.TLS.Domains, TraefikTLSDomain{})
	}

	subField := matches[2]
	switch subField {
	case "main":
		r.TLS.Domains[index].Main = value
	case "sans":
		r.TLS.Domains[index].SANs = splitComma(value)
	}
}

func parseServiceField(s *TraefikService, field, value string) {
	switch field {
	case "loadbalancer.server.port":
		if n, err := strconv.Atoi(value); err == nil {
			s.Port = n
		}
	case "loadbalancer.server.scheme":
		s.Scheme = value
	}
}

func parseMiddlewareField(mw *TraefikMiddleware, field, value string) {
	// field is like "headers.customrequestheaders.X-Forwarded-Proto" or "redirectscheme.scheme"
	parts := strings.SplitN(field, ".", 2)
	mwType := parts[0]

	if mw.Type == "" {
		mw.Type = mwType
	}

	if len(parts) == 2 {
		if mw.Config == nil {
			mw.Config = make(map[string]string)
		}

		mw.Config[parts[1]] = value
	}
}

func splitComma(s string) []string {
	raw := strings.Split(s, ",")
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func sortedValues[T any](m map[string]*T, name func(T) string) []T {
	result := make([]T, 0, len(m))
	for _, v := range m {
		result = append(result, *v)
	}
	slices.SortFunc(result, func(a, b T) int {
		return strings.Compare(name(a), name(b))
	})
	return result
}
