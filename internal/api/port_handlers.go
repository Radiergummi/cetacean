package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/filter"
)

// portList is the published ports as a list, each read as part of the service
// publishing it.
var portList = listSpec[cache.PublishedPort]{
	resourceType: "service",
	aclName:      cluster.PortACLName,
	searchName:   func(p cache.PublishedPort) string { return p.ServiceName },
	filterEnv:    filter.Ports,
	sortKeys: map[string]func(cache.PublishedPort) string{
		// Padded, as sort keys compare as strings and ports go up to 65535.
		"published": func(p cache.PublishedPort) string { return fmt.Sprintf("%05d", p.PublishedPort) },
		"target":    func(p cache.PublishedPort) string { return fmt.Sprintf("%05d", p.TargetPort) },
		"protocol":  func(p cache.PublishedPort) string { return string(p.Protocol) },
		"mode":      func(p cache.PublishedPort) string { return string(p.PublishMode) },
		"service":   func(p cache.PublishedPort) string { return p.ServiceName },
		"stack":     func(p cache.PublishedPort) string { return p.Stack },
	},
	itemType: "PublishedPort",
	idFunc:   portID,
	csv: func(w http.ResponseWriter, r *http.Request, page []cache.PublishedPort) {
		writeCSV(w, r, "ports", csvTableForPorts(page))
	},
}

// portID names a port as a fragment of its service: a port has no URL of its
// own, and two dynamic host ports of one service can share a published port.
func portID(p cache.PublishedPort) string {
	return fmt.Sprintf(
		"/services/%s#port-%d-%s-%d",
		p.ServiceID, p.PublishedPort, p.Protocol, p.TargetPort,
	)
}

func (h *Handlers) HandleListPorts(w http.ResponseWriter, r *http.Request) {
	spec := portList
	canRead := h.readChecker(r)
	spec.list = func() []cache.PublishedPort { return cluster.PublishedPorts(h.cache, canRead) }

	handleList(h, w, r, spec)
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
			string(p.Protocol),
			string(p.PublishMode),
			p.ServiceName,
			p.Stack,
			strings.Join(p.Nodes, " "),
			p.ServiceID,
		})
	}

	return table
}
