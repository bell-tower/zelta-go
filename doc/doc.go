// Package doc embeds Zelta's manual pages so "zelta help" works without an
// installed manual.
package doc

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed man7/*.7 man8/*.8 src/*.md
var files embed.FS

// Page is one manual page.
type Page struct {
	// Name is the page name, e.g. "zelta-backup".
	Name string
	// Section is "7" or "8".
	Section string
	// Roff is the page source in man(7) format.
	Roff []byte
	// Markdown is the human-readable source of the page.
	Markdown []byte
}

// Topic maps a "zelta help" topic to a manual page name and section.
func Topic(topic string) (name, section string) {
	switch topic {
	case "":
		return "zelta", "8"
	case "options":
		return "zelta-options", "7"
	case "zprune":
		return "zprune", "8"
	case "zmatch":
		return "zelta-match", "8"
	case "zpull", "zpush":
		return "zelta-backup", "8"
	default:
		return "zelta-" + topic, "8"
	}
}

// Lookup returns the embedded page for a manual name and section.
func Lookup(name, section string) (Page, bool) {
	roff, err := files.ReadFile(path.Join("man"+section, name+"."+section))
	if err != nil {
		return Page{}, false
	}
	md := name
	if section == "7" {
		md += ".7"
	}
	markdown, _ := files.ReadFile(path.Join("src", md+".md"))
	return Page{Name: name, Section: section, Roff: roff, Markdown: markdown}, true
}

// Topics lists the help topics that have manual pages.
func Topics() []string {
	var topics []string
	for _, dir := range []string{"man7", "man8"} {
		entries, _ := fs.ReadDir(files, dir)
		for _, e := range entries {
			name := strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".7"), ".8")
			switch {
			case name == "zelta":
			case name == "zelta-options":
				topics = append(topics, "options")
			case strings.HasPrefix(name, "zelta-"):
				topics = append(topics, strings.TrimPrefix(name, "zelta-"))
			default:
				topics = append(topics, name)
			}
		}
	}
	sort.Strings(topics)
	return topics
}

// FS exposes the embedded files (man7/, man8/, src/).
func FS() fs.FS { return files }
