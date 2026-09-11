package api

import (
	"net/http"
	"time"

	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cluster"
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

	rep, ok := representationOr404(w, r, "volume", name, h.volumeRepresentation)
	if !ok {
		return
	}

	created, _ := time.Parse(time.RFC3339, vol.CreatedAt)
	writeCachedJSONTimed(w, r, rep, created)
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
		itemType: "Volume",
		idFunc:   func(v volume.Volume) string { return "/volumes/" + v.Name },
		// The row builder takes pointers, because a cache volume is nilable
		// where a listed one never is.
		rows: func(volumes []volume.Volume) []cluster.Row {
			pointers := make([]*volume.Volume, len(volumes))
			for i := range volumes {
				pointers[i] = &volumes[i]
			}

			return cluster.RowsForVolumes(pointers)
		},
	})
}
