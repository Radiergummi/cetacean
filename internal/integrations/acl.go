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
	readVal, hasRead := labels[acl.LabelRead]
	writeVal, hasWrite := labels[acl.LabelWrite]

	if !hasRead && !hasWrite {
		return nil
	}

	integration := &ACLIntegration{Name: "cetacean-acl"}

	if hasRead {
		integration.Read = acl.ParseAudienceList(readVal)
	}
	if hasWrite {
		integration.Write = acl.ParseAudienceList(writeVal)
	}

	return integration
}
