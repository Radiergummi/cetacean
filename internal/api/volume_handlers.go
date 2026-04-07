package api

import (
	"net/http"
	"time"

	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Volumes ---

func (h *Handlers) HandleGetVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	vol, ok := lookupOr404(w, r, "volume", name, h.cache.GetVolume)
	if !ok {
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "volume:"+vol.Name) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}
	h.setAllow(w, r, "volume", vol.Name)
	created, _ := time.Parse(time.RFC3339, vol.CreatedAt)
	writeCachedJSONTimed(
		w,
		r,
		NewDetailResponse(r.Context(), "/volumes/"+name, "Volume", VolumeResponse{
			Volume: vol,
			Services: acl.Filter(
				h.acl,
				auth.IdentityFromContext(r.Context()),
				"read",
				h.cache.ServicesUsingVolume(name),
				func(ref cache.ServiceRef) string {
					return "service:" + ref.Name
				},
			),
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
