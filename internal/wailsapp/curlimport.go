package wailsapp

import (
	"freeman/internal/curlimport"
	"freeman/internal/domain"
)

// ImportCurl turns a pasted curl command into a request. It doesn't save
// it: the editor loads it unsaved, so what arrived can be looked at —
// and discarded — before it joins the collection.
func (a *App) ImportCurl(text string) (domain.Item, error) {
	return curlimport.Parse(text)
}
