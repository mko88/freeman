// Package domain holds Freeman's core data types (collections, requests,
// environments). It has no I/O and no dependency on any other internal
// package, so store, httpengine, script, and importer can each depend on it
// without depending on each other.
package domain

// ItemType discriminates between a folder (a group of items) and a request
// (a saved HTTP call) within a Collection's tree.
type ItemType string

const (
	ItemTypeFolder  ItemType = "folder"
	ItemTypeRequest ItemType = "request"
)

// Collection is a named tree of folders and requests, persisted as one
// collections/<slug>/collection.json file.
type Collection struct {
	FormatVersion string `json:"formatVersion"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Items         []Item `json:"items"`
}

// Item is either a folder (Items populated) or a request (the request
// fields populated), selected by Type. Both shapes share one struct rather
// than a Go interface so the on-disk JSON stays a flat, hand-editable tree.
type Item struct {
	Type ItemType `json:"type"`
	ID   string   `json:"id"`
	Name string   `json:"name"`

	// Folder fields.
	Items []Item `json:"items,omitempty"`

	// Request fields.
	Method           string       `json:"method,omitempty"`
	URL              string       `json:"url,omitempty"`
	Params           []QueryParam `json:"params,omitempty"`
	Headers          []Header     `json:"headers,omitempty"`
	Body             *Body        `json:"body,omitempty"`
	PreRequestScript string       `json:"preRequestScript,omitempty"`
	TestScript       string       `json:"testScript,omitempty"`
}

type QueryParam struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

type Header struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// FormFieldType selects whether a FormField's Value or its FilePath is
// what actually goes on the wire. "" is treated as FormFieldTypeText —
// existing saved data predates this field and has neither key.
type FormFieldType string

const (
	FormFieldTypeText FormFieldType = "text"
	FormFieldTypeFile FormFieldType = "file"
)

// FormField is one row of a form-data or x-www-form-urlencoded body. Both
// modes share this shape (and Body.FormFields) — they differ only in how
// httpengine.Execute encodes the enabled rows onto the wire. FilePath (a
// local path httpengine reads at execute time) is only meaningful for a
// FormFieldTypeFile row under form-data — x-www-form-urlencoded has no
// way to carry binary content, so a file-type row there just encodes as
// empty.
type FormField struct {
	Key      string        `json:"key"`
	Value    string        `json:"value"`
	Enabled  bool          `json:"enabled"`
	Type     FormFieldType `json:"type,omitempty"`
	FilePath string        `json:"filePath,omitempty"`
}

// BodyMode selects how Body's fields should be interpreted when building
// the outgoing request.
type BodyMode string

const (
	BodyModeNone       BodyMode = "none"
	BodyModeRaw        BodyMode = "raw"
	BodyModeForm       BodyMode = "form-data"
	BodyModeURLEncoded BodyMode = "x-www-form-urlencoded"
	BodyModeBinary     BodyMode = "binary"
)

type Body struct {
	Mode       BodyMode    `json:"mode"`
	Raw        string      `json:"raw,omitempty"`
	FormFields []FormField `json:"formFields,omitempty"`
	// BinaryFilePath is a local path httpengine reads at execute time and
	// sends as the entire request body, for BodyModeBinary.
	BinaryFilePath string `json:"binaryFilePath,omitempty"`
	// No RawContentType (retired 2026-09-04): a raw body's Content-Type is
	// a regular header (Item.Headers) like any other, set via
	// addRequestHeader/setRequestHeader — httpengine.buildBody no longer
	// synthesizes one for BodyModeRaw. form-data/urlencoded/binary still
	// each imply their own Content-Type (multipart boundary, the fixed
	// urlencoded MIME type, sniffed from the file) since those aren't
	// something a header row could express instead.
}

// UpsertItem replaces the item with a matching ID anywhere in the tree, or
// appends it to the collection's root if no match is found.
func (c *Collection) UpsertItem(item Item) {
	if replaceItem(c.Items, item) {
		return
	}
	c.Items = append(c.Items, item)
}

func replaceItem(items []Item, item Item) bool {
	for i := range items {
		if items[i].ID == item.ID {
			items[i] = item
			return true
		}
		if replaceItem(items[i].Items, item) {
			return true
		}
	}
	return false
}

// FindItem returns the item with the given ID anywhere in the tree.
func (c *Collection) FindItem(id string) *Item {
	return findItem(c.Items, id)
}

func findItem(items []Item, id string) *Item {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
		if found := findItem(items[i].Items, id); found != nil {
			return found
		}
	}
	return nil
}

// RemoveItem deletes the item with the given ID anywhere in the tree
// (including inside folders), reporting whether anything was removed.
func (c *Collection) RemoveItem(id string) bool {
	items, removed := removeItem(c.Items, id)
	c.Items = items
	return removed
}

func removeItem(items []Item, id string) ([]Item, bool) {
	for i := range items {
		if items[i].ID == id {
			// items[:i:i] caps the slice at i so this append can't
			// silently alias/overwrite the caller's backing array.
			return append(items[:i:i], items[i+1:]...), true
		}
	}
	for i := range items {
		updated, removed := removeItem(items[i].Items, id)
		if removed {
			items[i].Items = updated
			return items, true
		}
	}
	return items, false
}
