package integrations

import (
	"github.com/radiergummi/cetacean/internal/acl"
)

// ACLIntegration represents parsed Cetacean ACL label configuration.
type ACLIntegration struct {
	Name  string   `json:"name"`
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

func detectACL(labels map[string]string) *ACLIntegration {
	read, write := acl.ParseACLLabels(labels)
	if read == nil && write == nil {
		return nil
	}
	return &ACLIntegration{Name: "cetacean-acl", Read: read, Write: write}
}
