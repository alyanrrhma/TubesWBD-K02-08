package web

import "testing"

func TestLoadTemplates(t *testing.T) {
	if _, err := loadTemplates(); err != nil {
		t.Fatal(err)
	}
}
