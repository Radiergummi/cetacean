package mcp

import (
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// maxResourceLinks bounds how many resource_link items one result carries.
// Links are an affordance, not the payload — a host renders them as things to
// open, and the model already has every id in structuredContent — so the cap
// only bites on a page nobody drills into one row at a time.
const maxResourceLinks = 25

// resourceLinkFor builds the link to one resource from the singular type name.
// The plural half comes from describableResourceTypes, so a type in neither
// yields no link rather than a URI resolving to nothing; an empty name falls
// back to the id. qualifier fills the description, which is sent either way.
func resourceLinkFor(singularType, id, name, qualifier string) (mcplib.ResourceLink, bool) {
	plural, ok := describableResourceTypes[singularType]
	if !ok || id == "" {
		return mcplib.ResourceLink{}, false
	}

	if name == "" {
		name = id
	}

	description := singularType
	if qualifier != "" {
		description += " · " + qualifier
	}

	return mcplib.ResourceLink{
		Type:        mcplib.ContentTypeLink,
		URI:         "cetacean://" + plural + "/" + id,
		Name:        name,
		Description: description,
		MIMEType:    mcpMIMEType,
	}, true
}

// resourceLinksForRows offers one link per row a listing returned, bounded by
// maxResourceLinks. The rows are the ones actually returned, filtered and
// paged, so a caller is never handed a link to something the page it is
// reading does not mention.
func resourceLinksForRows(rows []cluster.Row) []mcplib.ResourceLink {
	if len(rows) > maxResourceLinks {
		rows = rows[:maxResourceLinks]
	}

	links := make([]mcplib.ResourceLink, 0, len(rows))

	for _, row := range rows {
		if link, ok := resourceLinkFor(row.Type, row.ID, row.Name, row.State); ok {
			links = append(links, link)
		}
	}

	return links
}

// resourceLinksForDigest offers the described resource plus everything it
// cross-references, which turns the names in a digest into somewhere the client
// can go. Related is already bounded by the resource's shape and filtered to
// what the caller may read, so the same bound applies here for free.
func resourceLinksForDigest(digest cluster.Digest) []mcplib.ResourceLink {
	links := make([]mcplib.ResourceLink, 0, 1+len(digest.Related))

	if link, ok := resourceLinkFor(digest.Type, digest.ID, digest.Name, digest.State); ok {
		links = append(links, link)
	}

	for _, related := range digest.Related {
		if len(links) >= maxResourceLinks {
			break
		}

		link, ok := resourceLinkFor(related.Type, related.ID, related.Name, related.Relation)
		if ok {
			links = append(links, link)
		}
	}

	return links
}

// withResourceLinks appends the links after the text item structuredToolResult
// put there. Order is deliberate: a client rendering sequentially shows the
// answer before the places to go next, and one predating resource_link ignores
// an item whose type it does not know rather than losing the answer to it.
func withResourceLinks(
	result *mcplib.CallToolResult,
	links []mcplib.ResourceLink,
) *mcplib.CallToolResult {
	for _, link := range links {
		result.Content = append(result.Content, link)
	}

	return result
}
