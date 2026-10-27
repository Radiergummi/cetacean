package api

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/network"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/ipam"
)

// --- Networks ---

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

	networks, _, p, ok := prepareList(h, w, r, spec, cluster.VersionSelection{})
	if !ok {
		return
	}

	if p.Sort == "addresses" {
		sortByAddressUse(networks, addresses, strings.EqualFold(p.Dir, "desc"))
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
		func(n cluster.NetworkListItem) string { return "/networks/" + n.ID },
		func(page []network.Summary) []cluster.NetworkListItem {
			return cluster.NetworkListItems(page, addresses)
		},
	)
}

// sortByAddressUse orders networks by the share of their addresses in use. A
// network without a figure sorts after every one with, in either direction,
// which a string sort key cannot express.
func sortByAddressUse(
	networks []network.Summary,
	addresses map[string]ipam.Addresses,
	descending bool,
) {
	share := func(n network.Summary) (ipam.Addresses, bool) {
		a, ok := addresses[n.ID]

		return a, ok && a.Usable > 0
	}

	slices.SortStableFunc(networks, func(x, y network.Summary) int {
		a, aOK := share(x)
		b, bOK := share(y)

		switch {
		case aOK != bOK && aOK:
			return -1
		case aOK != bOK:
			return 1
		case !aOK:
			return 0
		case descending:
			return cmp.Compare(b.Used*a.Usable, a.Used*b.Usable)
		default:
			return cmp.Compare(a.Used*b.Usable, b.Used*a.Usable)
		}
	})
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
