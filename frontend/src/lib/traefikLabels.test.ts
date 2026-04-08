import type { MiddlewareFormState, RouterFormState, ServiceFormState } from "./traefikLabels";
import { serializeTraefikLabels } from "./traefikLabels";
import { describe, expect, it } from "vitest";

const emptyRouters: RouterFormState[] = [];
const emptyServices: ServiceFormState[] = [];
const emptyMiddlewares: MiddlewareFormState[] = [];

describe("serializeTraefikLabels", () => {
  it("disabled with empty forms produces only traefik.enable=false", () => {
    const result = serializeTraefikLabels(false, emptyRouters, emptyServices, emptyMiddlewares);

    expect(result).toEqual({ "traefik.enable": "false" });
  });

  it("enabled with one router generates correct labels", () => {
    const routers: RouterFormState[] = [
      {
        name: "myapp",
        rule: "Host(`example.com`)",
        entrypoints: ["web", "websecure"],
        middlewares: [],
        service: "",
        priority: undefined,
        certResolver: "",
      },
    ];

    const result = serializeTraefikLabels(true, routers, emptyServices, emptyMiddlewares);

    expect(result["traefik.enable"]).toBe("true");
    expect(result["traefik.http.routers.myapp.rule"]).toBe("Host(`example.com`)");
    expect(result["traefik.http.routers.myapp.entrypoints"]).toBe("web,websecure");
    expect(result["traefik.http.routers.myapp.middlewares"]).toBeUndefined();
    expect(result["traefik.http.routers.myapp.service"]).toBeUndefined();
  });

  it("router with certResolver generates tls labels", () => {
    const routers: RouterFormState[] = [
      {
        name: "secure",
        rule: "Host(`secure.example.com`)",
        entrypoints: [],
        middlewares: [],
        service: "",
        priority: undefined,
        certResolver: "letsencrypt",
      },
    ];

    const result = serializeTraefikLabels(true, routers, emptyServices, emptyMiddlewares);

    expect(result["traefik.http.routers.secure.tls"]).toBe("true");
    expect(result["traefik.http.routers.secure.tls.certresolver"]).toBe("letsencrypt");
  });

  it("router with priority generates priority label", () => {
    const routers: RouterFormState[] = [
      {
        name: "high",
        rule: "PathPrefix(`/api`)",
        entrypoints: [],
        middlewares: ["auth"],
        service: "api-svc",
        priority: 10,
        certResolver: "",
      },
    ];

    const result = serializeTraefikLabels(true, routers, emptyServices, emptyMiddlewares);

    expect(result["traefik.http.routers.high.priority"]).toBe("10");
    expect(result["traefik.http.routers.high.middlewares"]).toBe("auth");
    expect(result["traefik.http.routers.high.service"]).toBe("api-svc");
  });

  it("service with port generates loadbalancer label", () => {
    const services: ServiceFormState[] = [{ name: "backend", port: 8080, scheme: "" }];

    const result = serializeTraefikLabels(true, emptyRouters, services, emptyMiddlewares);

    expect(result["traefik.http.services.backend.loadbalancer.server.port"]).toBe("8080");
    expect(result["traefik.http.services.backend.loadbalancer.server.scheme"]).toBeUndefined();
  });

  it("service with scheme generates scheme label", () => {
    const services: ServiceFormState[] = [{ name: "backend", port: 443, scheme: "https" }];

    const result = serializeTraefikLabels(true, emptyRouters, services, emptyMiddlewares);

    expect(result["traefik.http.services.backend.loadbalancer.server.scheme"]).toBe("https");
  });

  it("middleware generates correct nested key", () => {
    const middlewares: MiddlewareFormState[] = [
      {
        name: "ratelimit",
        type: "rateLimit",
        config: [
          ["average", "100"],
          ["burst", "50"],
        ],
      },
    ];

    const result = serializeTraefikLabels(true, emptyRouters, emptyServices, middlewares);

    expect(result["traefik.http.middlewares.ratelimit.rateLimit.average"]).toBe("100");
    expect(result["traefik.http.middlewares.ratelimit.rateLimit.burst"]).toBe("50");
  });

  it("omits blank middleware values", () => {
    const middlewares: MiddlewareFormState[] = [
      {
        name: "headers",
        type: "headers",
        config: [
          ["customRequestHeaders.X-Forwarded-Proto", "https"],
          ["emptyKey", "   "],
        ],
      },
    ];

    const result = serializeTraefikLabels(true, emptyRouters, emptyServices, middlewares);

    expect(
      result["traefik.http.middlewares.headers.headers.customRequestHeaders.X-Forwarded-Proto"],
    ).toBe("https");
    expect(result["traefik.http.middlewares.headers.headers.emptyKey"]).toBeUndefined();
  });
});
