package api

import (
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Volumes ---

func (h *Handlers) HandleGetVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	vol, ok := h.cache.GetVolume(name)
	if !ok {
		writeErrorCode(w, r, "VOL002", fmt.Sprintf("volume %q not found", name))
		return
	}
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse(r.Context(), "/volumes/"+name, "Volume", VolumeResponse{
			Volume:   vol,
			Services: h.cache.ServicesUsingVolume(name),
		}),
	)
}

func (h *Handlers) HandleListVolumes(w http.ResponseWriter, r *http.Request) {
	volumes := h.cache.ListVolumes()
	volumes = searchFilter(
		volumes,
		r.URL.Query().Get("search"),
		func(v volume.Volume) string { return v.Name },
	)
	var ok bool
	if volumes, ok = exprFilter(volumes, r.URL.Query().Get("filter"), filter.VolumeEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	volumes = sortItems(volumes, p.Sort, p.Dir, map[string]func(volume.Volume) string{
		"name":   func(v volume.Volume) string { return v.Name },
		"driver": func(v volume.Volume) string { return v.Driver },
		"scope":  func(v volume.Volume) string { return v.Scope },
	})
	resp := applyPagination(r.Context(), volumes, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}
