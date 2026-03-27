package integrations

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// TraefikIntegration represents parsed Traefik configuration from Docker labels.
type TraefikIntegration struct {
	Name        string              `json:"name"`
	Enabled     bool                `json:"enabled"`
	Routers     []TraefikRouter     `json:"routers,omitempty"`
	Services    []TraefikService    `json:"services,omitempty"`
	Middlewares []TraefikMiddleware  `json:"middlewares,omitempty"`
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

func detectTraefik(labels map[string]string) *TraefikIntegration {
	// Collect all traefik labels.
	traefikLabels := make(map[string]string)
	for k, v := range labels {
		if strings.HasPrefix(k, "traefik.") {
			traefikLabels[k] = v
		}
	}

	if len(traefikLabels) == 0 {
		return nil
	}

	integration := &TraefikIntegration{
		Name:    "traefik",
		Enabled: true,
	}

	// Check explicit enable flag.
	if val, ok := traefikLabels["traefik.enable"]; ok {
		integration.Enabled = val == "true"
	}

	// Parse HTTP routers, services, and middlewares.
	routerMap := make(map[string]*TraefikRouter)
	serviceMap := make(map[string]*TraefikService)
	middlewareMap := make(map[string]*TraefikMiddleware)

	for k, v := range traefikLabels {
		suffix, ok := strings.CutPrefix(k, "traefik.http.")
		if !ok {
			continue
		}

		parts := strings.SplitN(suffix, ".", 3)
		if len(parts) < 3 {
			continue
		}

		category := parts[0]
		name := parts[1]
		field := parts[2]

		switch category {
		case "routers":
			r := getOrCreateRouter(routerMap, name)
			parseRouterField(r, field, v)
		case "services":
			s := getOrCreateService(serviceMap, name)
			parseServiceField(s, field, v)
		case "middlewares":
			m := getOrCreateMiddleware(middlewareMap, name)
			parseMiddlewareField(m, field, v)
		}
	}

	integration.Routers = sortedRouters(routerMap)
	integration.Services = sortedServices(serviceMap)
	integration.Middlewares = sortedMiddlewares(middlewareMap)

	return integration
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
		Name:   name,
		Config: make(map[string]string),
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
		ensureTLS(r)
	case field == "tls.certresolver":
		ensureTLS(r)
		r.TLS.CertResolver = value
	case field == "tls.options":
		ensureTLS(r)
		r.TLS.Options = value
	case strings.HasPrefix(field, "tls.domains"):
		ensureTLS(r)
		parseTLSDomain(r.TLS, field, value)
	}
}

func ensureTLS(r *TraefikRouter) {
	if r.TLS == nil {
		r.TLS = &TraefikTLS{}
	}
}

func parseTLSDomain(tls *TraefikTLS, field, value string) {
	matches := tlsDomainIndexRegexp.FindStringSubmatch(field)
	if matches == nil {
		return
	}

	index, err := strconv.Atoi(matches[1])
	if err != nil {
		return
	}

	// Grow slice to accommodate index.
	for len(tls.Domains) <= index {
		tls.Domains = append(tls.Domains, TraefikTLSDomain{})
	}

	subField := matches[2]
	switch subField {
	case "main":
		tls.Domains[index].Main = value
	case "sans":
		tls.Domains[index].SANs = splitComma(value)
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

func sortedRouters(m map[string]*TraefikRouter) []TraefikRouter {
	result := make([]TraefikRouter, 0, len(m))
	for _, r := range m {
		result = append(result, *r)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func sortedServices(m map[string]*TraefikService) []TraefikService {
	result := make([]TraefikService, 0, len(m))
	for _, s := range m {
		result = append(result, *s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func sortedMiddlewares(m map[string]*TraefikMiddleware) []TraefikMiddleware {
	result := make([]TraefikMiddleware, 0, len(m))
	for _, mw := range m {
		result = append(result, *mw)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}
