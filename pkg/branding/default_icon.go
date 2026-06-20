package branding

import _ "embed"

//go:embed default_icon.png
var defaultIconPNG []byte

// defaultIconEtag is a fixed sentinel etag for the built-in icon (stable across
// boots so caches/304s work).
const defaultIconEtag = "default"
