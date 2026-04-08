package api

import (
	"net/http"
	"time"

	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Volumes ---

func (h *Handlers) HandleGetVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	vol, ok := lookupACL(h, w, r, "volume", name, h.cache.GetVolume, func(v volume.Volume) string {
		return "volume:" + v.Name
	})
	if !ok {
		return
	}
	h.setAllow(w, r, "volume", vol.Name)
	created, _ := time.Parse(time.RFC3339, vol.CreatedAt)
	writeCachedJSONTimed(
		w,
		r,
		NewDetailResponse(r.Context(), "/volumes/"+name, "Volume", VolumeResponse{
			Volume:   vol,
			Services: h.filterServiceRefs(r, h.cache.ServicesUsingVolume(name)),
		}),
		created,
	)
}

func (h *Handlers) HandleListVolumes(w http.ResponseWriter, r *http.Request) {
	handleList(h, w, r, listSpec[volume.Volume]{
		resourceType: "volume",
		linkTemplate: "/volumes/{name}",
		list:         h.cache.ListVolumes,
		aclResource:  func(v volume.Volume) string { return "volume:" + v.Name },
		searchName:   func(v volume.Volume) string { return v.Name },
		filterEnv:    filter.VolumeEnv,
		sortKeys: map[string]func(volume.Volume) string{
			"name":   func(v volume.Volume) string { return v.Name },
			"driver": func(v volume.Volume) string { return v.Driver },
			"scope":  func(v volume.Volume) string { return v.Scope },
		},
	})
}
