package app

import (
	"strings"

	"github.com/moby/moby/api/types/container"
)

type containerRef struct {
	Name string
	ID   string
}

func containerRefFromSummary(c container.Summary) containerRef {
	name := "<unknown>"
	if len(c.Names) > 0 {
		name = shortName(c.Names[0])
	}
	return containerRef{Name: name, ID: shortID(c.ID)}
}

func containerRefFromInspect(cur container.InspectResponse) containerRef {
	return containerRef{Name: shortName(cur.Name), ID: shortID(cur.ID)}
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func shortName(name string) string {
	if name == "" {
		return "<noname>"
	}
	return strings.TrimPrefix(name, "/")
}
