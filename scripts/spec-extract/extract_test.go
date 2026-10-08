package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// page is a paginated RFC in miniature: a table of contents whose entries
// carry dot leaders, a page break with the running header and footer around
// it, and two sections of which only one is declared.
const page = `Network Working Group                                        A. Author
Request for Comments: 9999                                 January 2026


Table of Contents

   1.  Introduction . . . . . . . . . . . . . . . . . . . . . . . .   2
   4.  Protocol . . . . . . . . . . . . . . . . . . . . . . . . . .   3

1.  Introduction

   This document describes nothing and the server MAY ignore it.

4.  Protocol

   The server MUST answer. It may also answer twice, e.g. on a retry.

Author                    Standards Track                      [Page 1]

` + "\f" + `
RFC 9999                      Nothing                     January 2026


4.1.  Parameters

   token
      REQUIRED.  The token.

5.  Security

   Everything here MUST be ignored by the extractor.
`

func TestStatementsReadsOnlyTheDeclaredSections(t *testing.T) {
	got := Statements(page, []string{"4"})

	want := []string{
		"The server MUST answer.",
		"token REQUIRED.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("statements = %q, want %q", got, want)
	}
}

// The table of contents names every section, so reading it as headings would
// put the whole document inside whichever section was declared last.
func TestTheTableOfContentsIsNotAHeading(t *testing.T) {
	for _, s := range Statements(page, []string{"1"}) {
		if strings.Contains(s, "MUST answer") {
			t.Errorf("section 4 leaked into section 1: %q", s)
		}
	}
}

// The running header and footer sit inside the text and say nothing, and the
// header would otherwise join the paragraph that follows it.
func TestPaginationFurnitureIsDropped(t *testing.T) {
	for _, line := range depaginate(page) {
		if strings.Contains(line, "[Page ") || strings.HasPrefix(line, "RFC 9999 ") {
			t.Errorf("furniture survived: %q", line)
		}
	}
}

func TestSentencesKeepsAbbreviationsWhole(t *testing.T) {
	got := sentences("The server MUST answer. It may also answer twice, e.g. on a retry.")

	want := []string{
		"The server MUST answer.",
		"It may also answer twice, e.g. on a retry.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("sentences = %q, want %q", got, want)
	}
}

// Lower case carries no obligation, which RFC 8174 makes a rule rather than a
// convention.
func TestOnlyUpperCaseKeywordsCount(t *testing.T) {
	if got := Statements("\n1.  X\n\n   The server may answer.\n", []string{"1"}); len(got) != 0 {
		t.Errorf("statements = %q, want none", got)
	}
}

func TestSourceURL(t *testing.T) {
	cases := map[string]struct {
		doc  spec.Document
		want string
	}{
		"an RFC by number": {
			doc:  spec.Document{Source: "RFC 7636"},
			want: "https://www.rfc-editor.org/rfc/rfc7636.txt",
		},
		"a draft by revision": {
			doc:  spec.Document{Source: "OAuth 2.1", Revision: "draft-ietf-oauth-v2-1-16"},
			want: "https://www.ietf.org/archive/id/draft-ietf-oauth-v2-1-16.txt",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := sourceURL(&tc.doc)
			if err != nil {
				t.Fatalf("sourceURL: %v", err)
			}
			if got != tc.want {
				t.Errorf("url = %q, want %q", got, tc.want)
			}
		})
	}

	if _, err := sourceURL(&spec.Document{Source: "OpenID Connect Core 1.0"}); err == nil {
		t.Error("a document with no plain-text form must say so")
	}
}
