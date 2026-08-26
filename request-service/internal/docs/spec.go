// Package docs holds this service's generated OpenAPI document.
//
// swagger.json is produced by `swag init` from the annotations on the handlers, so the
// document cannot drift from the code it describes - regenerate it after changing a
// handler's signature or its annotation block:
//
//	swag init --v3.1 -g cmd/server/main.go -o internal/docs -ot json
//
// Only the JSON is generated (-ot json): swag's docs.go would pull the swag runtime
// into the binary purely to hand back a string this package can embed for free.
package docs

import _ "embed"

// SpecJSON is the OpenAPI 3.1 document served at /v3/api-docs.
//
//go:embed swagger.json
var SpecJSON []byte
