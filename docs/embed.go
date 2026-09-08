package docs

import _ "embed"

// OpenAPI is the embedded API contract served by the API process.
//
//go:embed openapi.yaml
var OpenAPI []byte
