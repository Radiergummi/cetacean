package integrations

import "strings"

// ACLIntegration represents parsed Cetacean ACL label configuration.
type ACLIntegration struct {
	Name  string   `json:"name"`
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

func detectACL(labels map[string]string) *ACLIntegration {
	readVal, hasRead := labels["cetacean.acl.read"]
	writeVal, hasWrite := labels["cetacean.acl.write"]

	if !hasRead && !hasWrite {
		return nil
	}

	integration := &ACLIntegration{Name: "cetacean-acl"}

	if hasRead {
		integration.Read = splitAudiences(readVal)
	}
	if hasWrite {
		integration.Write = splitAudiences(writeVal)
	}

	return integration
}

func splitAudiences(value string) []string {
	if value == "" {
		return nil
	}

	var result []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
