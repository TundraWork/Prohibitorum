package webui

import (
	"bytes"
	"encoding/json"
	"html"
	"io/fs"
	"strings"
)

// routeChunksFile is written by the dashboard build
// (dashboard/tools/route-chunks.ts): the public routes' paths, and for each
// group of routes the code a page of that group loads after the shell's own
// script.
const routeChunksFile = "route-chunks.json"

type routeChunks struct {
	PublicPaths []string            `json:"publicPaths"`
	Preload     map[string][]string `json:"preload"`
}

// shells is the SPA shell for each group of routes, split on the instance-name
// placeholder. A public page and a console page get the same shell, each with
// `modulepreload` links for its own group's code, so that code downloads
// beside the shell's script instead of after the router asks for it.
type shells struct {
	public, console [][]byte
	publicPaths     [][]string
}

// loadShells builds the shells from index.html and route-chunks.json. A build
// without the file (or with one it cannot read) serves the plain shell to every
// page; the code then loads when the router asks for it.
func loadShells(sub fs.FS, index []byte) shells {
	plain := bytes.Split(index, []byte("__INSTANCE_NAME__"))
	out := shells{public: plain, console: plain}
	raw, err := fs.ReadFile(sub, routeChunksFile)
	if err != nil {
		return out
	}
	var chunks routeChunks
	if json.Unmarshal(raw, &chunks) != nil {
		return out
	}
	withPreloads := func(files []string) [][]byte {
		var links strings.Builder
		for _, file := range files {
			links.WriteString(`<link rel="modulepreload" crossorigin href="`)
			links.WriteString(html.EscapeString(file))
			links.WriteString(`">`)
		}
		shell := bytes.Replace(index, []byte("</head>"), []byte(links.String()+"</head>"), 1)
		return bytes.Split(shell, []byte("__INSTANCE_NAME__"))
	}
	out.public = withPreloads(chunks.Preload["public"])
	out.console = withPreloads(chunks.Preload["console"])
	for _, pattern := range chunks.PublicPaths {
		out.publicPaths = append(out.publicPaths, pathSegments(pattern))
	}
	return out
}

// forPath picks the shell for a page address: a public route's, or else the
// console's, which also covers an address no route has.
func (s shells) forPath(urlPath string) [][]byte {
	segments := pathSegments(urlPath)
	for _, pattern := range s.publicPaths {
		if matchesPattern(pattern, segments) {
			return s.public
		}
	}
	return s.console
}

func pathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// matchesPattern compares a path against a route path, where a `$name`
// segment stands for any one non-empty segment.
func matchesPattern(pattern, segments []string) bool {
	if len(pattern) != len(segments) {
		return false
	}
	for i, want := range pattern {
		if strings.HasPrefix(want, "$") {
			if segments[i] == "" {
				return false
			}
			continue
		}
		if want != segments[i] {
			return false
		}
	}
	return true
}
