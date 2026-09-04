package domain

import "testing"

func TestRemoveItemTopLevel(t *testing.T) {
	c := &Collection{Items: []Item{
		{Type: ItemTypeRequest, ID: "r_1", Name: "One"},
		{Type: ItemTypeRequest, ID: "r_2", Name: "Two"},
		{Type: ItemTypeRequest, ID: "r_3", Name: "Three"},
	}}

	if !c.RemoveItem("r_2") {
		t.Fatal("expected RemoveItem to report true")
	}
	if len(c.Items) != 2 {
		t.Fatalf("expected 2 items left, got %d: %+v", len(c.Items), c.Items)
	}
	if c.FindItem("r_2") != nil {
		t.Fatal("r_2 should be gone")
	}
	if c.FindItem("r_1") == nil || c.FindItem("r_3") == nil {
		t.Fatal("removing r_2 should not disturb its siblings")
	}
}

func TestRemoveItemNestedInFolder(t *testing.T) {
	c := &Collection{Items: []Item{
		{
			Type: ItemTypeFolder, ID: "f_1", Name: "Auth",
			Items: []Item{
				{Type: ItemTypeRequest, ID: "r_1", Name: "Login"},
				{Type: ItemTypeRequest, ID: "r_2", Name: "Logout"},
			},
		},
	}}

	if !c.RemoveItem("r_1") {
		t.Fatal("expected RemoveItem to report true for a nested item")
	}
	folder := c.FindItem("f_1")
	if folder == nil {
		t.Fatal("folder should still exist")
	}
	if len(folder.Items) != 1 || folder.Items[0].ID != "r_2" {
		t.Fatalf("expected only r_2 left in the folder, got %+v", folder.Items)
	}
}

func TestRemoveItemNotFound(t *testing.T) {
	c := &Collection{Items: []Item{{Type: ItemTypeRequest, ID: "r_1", Name: "One"}}}

	if c.RemoveItem("does-not-exist") {
		t.Fatal("expected RemoveItem to report false for an unknown ID")
	}
	if len(c.Items) != 1 {
		t.Fatalf("collection should be untouched, got %+v", c.Items)
	}
}
