package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/filter"
)

// portList is the published ports as a list. Each port is read as part of the
// service publishing it, so a caller sees the ports of the services they may
// read, and the CSV gets columns of its own rather than a Row's.
var portList = listSpec[cache.PublishedPort]{
	resourceType: "service",
	aclName:      func(p cache.PublishedPort) string { return p.ServiceName },
	searchName:   func(p cache.PublishedPort) string { return p.ServiceName },
	filterEnv:    filter.Ports,
	sortKeys: map[string]func(cache.PublishedPort) string{
		// Padded, as sort keys compare as strings and ports go up to 65535.
		"published": func(p cache.PublishedPort) string { return fmt.Sprintf("%05d", p.PublishedPort) },
		"target":    func(p cache.PublishedPort) string { return fmt.Sprintf("%05d", p.TargetPort) },
		"protocol":  func(p cache.PublishedPort) string { return p.Protocol },
		"mode":      func(p cache.PublishedPort) string { return p.PublishMode },
		"service":   func(p cache.PublishedPort) string { return p.ServiceName },
		"stack":     func(p cache.PublishedPort) string { return p.Stack },
	},
	itemType: "PublishedPort",
	idFunc:   portID,
}

// portID names a port as a fragment of its service: a port has no URL of its
// own, and two services may bind the same host-mode port on different nodes.
func portID(p cache.PublishedPort) string {
	return fmt.Sprintf("/services/%s#port-%d-%s", p.ServiceID, p.PublishedPort, p.Protocol)
}

func (h *Handlers) HandleListPorts(w http.ResponseWriter, r *http.Request) {
	spec := portList
	spec.list = h.cache.ListPublishedPorts

	validator := h.derivedETag(r)
	if listNotModified(h, w, r, spec, validator) {
		return
	}

	r, validator = h.ifRange(r, validator)

	items, p, ok := prepareList(h, w, r, spec)
	if !ok {
		return
	}

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		writeCSV(w, r, "ports", csvTableForPorts(csvPage(items, p)))
		return
	}

	writeListPage(
		w,
		r,
		items,
		p,
		validator,
		spec.itemType,
		spec.idFunc,
		func(page []cache.PublishedPort) []cache.PublishedPort {
			return page
		},
	)
}

func csvTableForPorts(ports []cache.PublishedPort) csvTable {
	table := csvTable{
		header: []string{
			"published",
			"target",
			"protocol",
			"mode",
			"service",
			"stack",
			"nodes",
			"service id",
		},
		records: make([][]string, 0, len(ports)),
	}

	for _, p := range ports {
		table.records = append(table.records, []string{
			strconv.FormatUint(uint64(p.PublishedPort), 10),
			strconv.FormatUint(uint64(p.TargetPort), 10),
			p.Protocol,
			p.PublishMode,
			p.ServiceName,
			p.Stack,
			strings.Join(p.Nodes, " "),
			p.ServiceID,
		})
	}

	return table
}
