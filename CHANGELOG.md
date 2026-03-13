# Changelog

All notable changes to Cetacean will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-03-12

### Added
- Per-resource SSE streaming on all list and detail endpoints
- Deterministic JSON-LD serialization with stable ETags (RFC 9110)
- OpenAPI 3.1 spec with Scalar API playground at `/api`
- JSON-LD `@context`, `@id`, `@type` metadata on all responses
- RFC 9457 problem details for structured error responses
- Content negotiation via `Accept` header or `.json`/`.html` extension
- ETag conditional caching (SHA-256) with 304 Not Modified support
- Global cross-resource search with `Cmd+K` command palette
- Network topology view (logical service connections + physical placement)
- Stack summary endpoint and detail pages with member resources
- Log viewer with live SSE streaming, regex search, JSON formatting
- Monitoring auto-detection (Prometheus, cAdvisor, node-exporter)
- Node resource gauges (CPU, memory, disk) with Prometheus metrics
- Service and stack metrics panels with time range selection
- Disk snapshot persistence for instant dashboard on restart
- Self-discovery Link headers (RFC 8288, RFC 8631)
- Expression-based filtering via `expr-lang/expr`
- Virtual scrolling for large tables (100+ rows)
- Activity feed with recent resource change history
- Multi-platform Docker images (amd64, arm64) with SBOM and provenance

### Security
- Secret values are never exposed in API responses
- Prometheus proxy restricted to `/query` and `/query_range` paths
- Security headers: `X-Content-Type-Options`, `X-Frame-Options`
- Connection limits: 256 SSE clients, 128 concurrent log streams
