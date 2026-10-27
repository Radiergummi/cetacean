package cluster

import (
	"encoding/json"

	"github.com/docker/docker/api/types/network"

	"github.com/radiergummi/cetacean/internal/ipam"
)

// NetworkListItem is a network as a listing serves it: Docker's fields with
// the address figure beside them, absent for a network that has none.
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

// NetworkListItems joins each network to its figure in addresses.
func NetworkListItems(
	networks []network.Summary,
	addresses map[string]ipam.Addresses,
) []NetworkListItem {
	items := make([]NetworkListItem, len(networks))

	for i, net := range networks {
		items[i] = NetworkListItem{Summary: net}
		if a, ok := addresses[net.ID]; ok {
			items[i].Addresses = &a
		}
	}

	return items
}
