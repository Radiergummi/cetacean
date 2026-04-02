package atom

import (
	"encoding/xml"
	"io"
	"time"
)

const namespace = "http://www.w3.org/2005/Atom"

// Feed is an Atom 1.0 feed document (RFC 4287).
type Feed struct {
	XMLName xml.Name  `xml:"feed"`
	NS      string    `xml:"xmlns,attr"`
	Title   string    `xml:"title"`
	ID      string    `xml:"id"`
	Updated time.Time `xml:"updated"`
	Links   []Link    `xml:"link"`
	Entries []Entry   `xml:"entry"`
}

// ContentElement renders <content type="text">...</content> per RFC 4287.
type ContentElement struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

// Entry is a single Atom feed entry.
type Entry struct {
	ID         string         `xml:"id"`
	Title      string         `xml:"title"`
	Updated    time.Time      `xml:"updated"`
	Content    ContentElement `xml:"content"`
	Links      []Link         `xml:"link"`
	Categories []Category     `xml:"category"`
}

// Link is an Atom link element.
type Link struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr,omitempty"`
}

// Category is an Atom category element.
type Category struct {
	Term string `xml:"term,attr"`
}

// Render writes the feed as Atom XML to w.
func Render(w io.Writer, f Feed) error {
	f.NS = namespace
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	return enc.Encode(f)
}
