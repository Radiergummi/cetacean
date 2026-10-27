package api

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/docker/docker/api/types/network"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/ipam"
)

// --- Networks ---

type NetworkListItem struct {
	network.Summary

	Addresses *ipam.Addresses `json:"Addresses,omitempty"`
}

// MarshalJSON appends Addresses to the network's own fields by hand: goccy/
// go-json SIGSEGVs encoding a network.Summary embedded in a struct.
func (n NetworkListItem) MarshalJSON() ([]byte, error) {
	body, err := json.Marshal(n.Summary)
	if err != nil || n.Addresses == nil {
		return body, err
	}

	addresses, err := json.Marshal(n.Addresses)
	if err != nil {
		return nil, err
	}

	body = append(body[:len(body)-1], `,"Addresses":`...)
	body = append(body, addresses...)

	return append(body, '}'), nil
}

func (h *Handlers) HandleGetNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	net, ok := lookupACL(
		h,
		w,
		r,
		"network",
		id,
		h.cache.GetNetwork,
		func(n network.Summary) string {
			return "network:" + n.Name
		},
	)
	if !ok {
		return
	}
	h.setAllow(w, r, "network", net.Name)

	rep, ok := representationOr404(w, r, "network", id, h.networkRepresentation)
	if !ok {
		return
	}

	writeCachedJSON(w, r, rep)
}

func (h *Handlers) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	// Read only once the validator is taken, so a body is never older than
	// its ETag; a 304 skips the count altogether.
	var addresses map[string]ipam.Addresses

	spec := listSpec[network.Summary]{
		resourceType: "network",
		linkTemplate: "/networks/{id}",
		list:         h.cache.ListNetworks,
		aclName:      func(n network.Summary) string { return n.Name },
		searchName:   func(n network.Summary) string { return n.Name },
		filterEnv:    filter.Networks,
		sortKeys: map[string]func(network.Summary) string{
			"name":   func(n network.Summary) string { return n.Name },
			"driver": func(n network.Summary) string { return n.Driver },
			"scope":  func(n network.Summary) string { return n.Scope },
			"addresses": func(n network.Summary) string {
				a, ok := addresses[n.ID]
				if !ok || a.Usable == 0 {
					return ""
				}

				return fmt.Sprintf("%020d", a.Used*1_000_000/a.Usable)
			},
		},
	}

	// The addresses come from the same cache, so the derived validator covers
	// them; see handleList.
	validator := h.derivedETag(r)
	if listNotModified(h, w, r, spec, validator) {
		return
	}

	r, validator = h.ifRange(r, validator)
	addresses = h.cache.NetworkAddresses()

	networks, p, ok := prepareList(h, w, r, spec)
	if !ok {
		return
	}

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		writeCSV(w, r, "networks", csvTableForNetworks(csvPage(networks, p), addresses))

		return
	}

	writeLinkTemplate(w, r, spec.linkTemplate)
	writeListPage(
		w,
		r,
		networks,
		p,
		validator,
		"Network",
		func(n NetworkListItem) string { return "/networks/" + n.ID },
		func(page []network.Summary) []NetworkListItem {
			items := make([]NetworkListItem, len(page))
			for i, net := range page {
				items[i] = NetworkListItem{Summary: net}
				if a, ok := addresses[net.ID]; ok {
					items[i].Addresses = &a
				}
			}

			return items
		},
	)
}

// csvTableForNetworks is the rows' table with the address figure before the
// ID, left empty for a network that has none.
func csvTableForNetworks(networks []network.Summary, addresses map[string]ipam.Addresses) csvTable {
	table := csvTableForRows("network", csvRowsInOrder(networks, cluster.RowsForNetworks))
	last := len(table.header) - 1

	table.header = slices.Insert(table.header, last, "used", "usable")

	for i, net := range networks {
		used, usable := "", ""
		if a, ok := addresses[net.ID]; ok {
			used, usable = strconv.Itoa(a.Used), strconv.Itoa(a.Usable)
		}

		table.records[i] = slices.Insert(table.records[i], last, used, usable)
	}

	return table
}
