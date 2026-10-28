package mcp

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/docker"
)

// versionArg is the optional `version` argument every tool editing an existing
// record of kind takes: the MCP counterpart of If-Match.
func versionArg(kind string) mcplib.ToolOption {
	return mcplib.WithInteger("version",
		mcplib.Min(0),
		mcplib.Description(fmt.Sprintf(
			"The %[1]s's `version` as you last read it, from describe or an earlier write's "+
				"result. Swarm also moves it on its own while it carries out a change, such as "+
				"a rolling update, so describe the %[1]s again after one. When given, the write "+
				"is applied only if the %[1]s is still at that version; if it has changed since, "+
				"the call is refused with the current version and nothing is written. Omit it to "+
				"write whatever the %[1]s holds now.",
			kind,
		)),
	)
}

// condition is the version a caller made a write conditional on, if any.
type condition struct {
	kind, id, name string
	version        uint64
	set            bool
}

// pinVersion makes the write conditional on the `version` argument, if given,
// and returns the ID the write must address: the pin is keyed by the resolved
// ID, so a write by name or to a record the cache cannot resolve would go out
// unpinned. Without the argument, arg is returned as given.
func (s *Server) pinVersion(
	ctx context.Context,
	req mcplib.CallToolRequest,
	kind, arg string,
) (context.Context, string, condition, error) {
	raw, present := req.GetArguments()["version"]
	if !present || raw == nil {
		return ctx, arg, condition{kind: kind, id: arg}, nil
	}

	number, ok := raw.(float64)
	if !ok || number < 0 || number != math.Trunc(number) || number > 1<<53 {
		return ctx, arg, condition{}, fmt.Errorf(
			"version must be a non-negative integer, the version index describe reports; got %v",
			raw,
		)
	}

	if !cluster.Versioned(kind) {
		return ctx, arg, condition{}, fmt.Errorf("a %s write cannot be made conditional", kind)
	}

	record, found, err := cluster.Lookup(s.cache, kind, arg)
	if err != nil {
		return ctx, arg, condition{}, err
	}

	if !found {
		return ctx, arg, condition{}, fmt.Errorf("%s %q not found", kind, arg)
	}

	c := condition{kind: kind, id: record.ID, name: record.Name, version: uint64(number), set: true}

	return docker.WithPinnedVersion(ctx, kind, c.id, swarm.Version{Index: c.version}), c.id, c, nil
}

// pinChecked is docker.PinChecked for a write the caller may also have made
// conditional. The two pins must agree, or the check validated a spec other
// than the one the write is applied to.
func (s *Server) pinChecked(
	ctx context.Context,
	c condition,
	svc swarm.Service,
) (context.Context, error) {
	if !c.set || c.version == svc.Version.Index {
		return docker.PinChecked(ctx, "service", svc.ID, svc.Version), nil
	}

	if svc.Version.Index > c.version {
		return ctx, c.changed(svc.Version.Index)
	}

	return ctx, fmt.Errorf(
		"service %q: Cetacean has not caught up with version %d yet (it holds %d), "+
			"and this write is checked against what it holds; nothing was written. Retry in a "+
			"moment, and if it is refused again, describe the service and use the version it reports",
		c.name, c.version, svc.Version.Index,
	)
}

// refused reports a write the engine turned away for being out of sequence as
// the caller's version having gone stale. Any other error goes through
// writeFailure.
func (s *Server) refused(ctx context.Context, c condition, err error) error {
	refusal, ok := cluster.ClassifyWrite(err, c.kind, c.id)
	if !ok {
		return err
	}
	if !c.set || refusal.Kind != cluster.Stale {
		return refusalError(ctx, refusal)
	}

	if stale, ok := errors.AsType[*docker.StaleVersionError](err); ok {
		return c.changed(stale.Current)
	}

	// The record moved between the writer's read and its write, so the cache
	// is the best account there is of where it went.
	record, _, _ := cluster.Lookup(s.cache, c.kind, c.id)
	current := record.Version
	if current == c.version {
		current = 0
	}

	return c.changed(current)
}

func (c condition) changed(current uint64) error {
	now := "it has moved past it"
	if current != 0 {
		now = fmt.Sprintf("it is now at version %d", current)
	}

	return fmt.Errorf(
		"%s %q changed since you read it: you passed version %d, and %s. "+
			"Nothing was written; describe it again and redo the change against what it holds now",
		c.kind, c.name, c.version, now,
	)
}
