package mcp

import (
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// maxResourceLinks bounds how many resource_link items one result carries.
//
// Links are an affordance, not the payload: a host renders them as things to
// open, and a hundred of those is worse than none, while the model already has
// every id in the result's own structuredContent. The cap therefore only bites
// on a page big enough that nobody is drilling into it one row at a time —
// find defaults to 200 records for the table widget, and that is a listing to
// tabulate rather than a shortlist to traverse.
const maxResourceLinks = 25

// resourceLinkFor builds the link to one resource, given the singular type
// name cluster.Row and cluster.Related both use.
//
// The plural half of the URI comes from describableResourceTypes — the same
// map describe derives its argument from — so a type reachable by one is
// reachable by the other, and a type in neither yields no link rather than a
// URI that resolves to nothing. name may be empty (an unplaced global task
// resolves to nothing better than its id), and the id stands in for it, since
// ResourceLink.name is required by the schema.
//
// qualifier is whatever distinguishes this link beyond its type — a row's
// state, a cross-reference's relation — and may be empty. It goes into the
// description because that field has no omitempty in the wire type and so is
// sent either way: a host rendering a link as a chip gets "service · running"
// for the same bytes it would otherwise spend on "".
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
// maxResourceLinks.
//
// The rows are the ones actually returned — filtered and paged — so the links
// and the records describe the same set, and a caller cannot be handed a link
// to something the page it is reading does not mention.
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
// cross-references, which is the traversal describe exists to enable: a task's
// digest names its service and its node, and this is what turns those names
// into somewhere the client can actually go.
//
// Related is already bounded by the resource's own shape and already filtered
// to what the caller may read — the digest builders never name a parent from
// behind the caller's grants — so the same bound applies here for free.
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

// withResourceLinks appends the links to a result's content, after the text
// item structuredToolResult put there.
//
// Order is deliberate: a client that renders content sequentially shows the
// answer before the places to go next, and a client on a protocol revision
// that predates resource_link ignores an item whose type it does not know
// rather than losing the answer to it.
func withResourceLinks(
	result *mcplib.CallToolResult,
	links []mcplib.ResourceLink,
) *mcplib.CallToolResult {
	for _, link := range links {
		result.Content = append(result.Content, link)
	}

	return result
}
