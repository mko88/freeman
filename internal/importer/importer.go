// Package importer defines the seam for converting external formats
// (Postman Collection v2.1, OpenAPI) into a domain.Collection. No
// implementation ships in v1.
package importer

import "freeman/internal/domain"

// Importer converts external collection/spec data into a domain.Collection.
type Importer interface {
	Import(data []byte) (*domain.Collection, error)
}
