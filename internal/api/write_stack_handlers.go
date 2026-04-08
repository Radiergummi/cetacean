package api

import (
	"context"
	"log/slog"
	"net/http"

	cerrdefs "github.com/containerd/errdefs"
)

// removeAll removes each resource by ID, skipping not-found errors and
// collecting others into errs. Returns the count of successful removals.
func removeAll(
	ctx context.Context,
	ids []string,
	resourceType string,
	removeFn func(context.Context, string) error,
	errs []removeError,
) (int, []removeError) {
	var count int
	for _, id := range ids {
		if err := removeFn(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: resourceType, ID: id, Error: err.Error()})
			continue
		}
		count++
	}
	return count, errs
}

type removeError struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Error string `json:"error"`
}

type removeStackResponse struct {
	Removed struct {
		Services int `json:"services"`
		Networks int `json:"networks"`
		Configs  int `json:"configs"`
		Secrets  int `json:"secrets"`
	} `json:"removed"`
	Errors []removeError `json:"errors,omitempty"`
}

func (h *Handlers) HandleRemoveStack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	stack, ok := lookupOr404(w, r, "stack", name, h.cache.GetStack)
	if !ok {
		return
	}

	slog.Info("removing stack", "stack", name,
		"services", len(stack.Services),
		"networks", len(stack.Networks),
		"configs", len(stack.Configs),
		"secrets", len(stack.Secrets),
	)

	ctx := r.Context()
	var resp removeStackResponse
	var errs []removeError

	resp.Removed.Services, errs = removeAll(
		ctx,
		stack.Services,
		"service",
		h.serviceLifecycle.RemoveService,
		errs,
	)
	resp.Removed.Networks, errs = removeAll(
		ctx,
		stack.Networks,
		"network",
		h.resourceRemover.RemoveNetwork,
		errs,
	)
	resp.Removed.Secrets, errs = removeAll(
		ctx,
		stack.Secrets,
		"secret",
		h.secretWriter.RemoveSecret,
		errs,
	)
	resp.Removed.Configs, errs = removeAll(
		ctx,
		stack.Configs,
		"config",
		h.configWriter.RemoveConfig,
		errs,
	)

	if len(errs) > 0 {
		resp.Errors = errs
	}

	writeMutationResponse(w, r, resp)
}
