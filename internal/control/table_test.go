package control

import (
	"strings"
	"testing"

	"voidbleed/internal/theme"
)

// Every column is padded to its full width, which left the row exactly as
// wide as the table and no room for the badge -- so "→ 3.2.8", "orphan" and
// the rest were silently dropped.
func TestRowBadgeSurvivesColumnPadding(t *testing.T) {
	tbl := Table{Headers: []string{"application", "version", ""}, Widths: []int{0, 14, 10}}
	tbl.SetRows([]Row{{ID: "org.gimp.GIMP", Cols: []string{"GIMP", "3.2.6", ""}, Badge: "→ 3.2.8"}})
	out := tbl.View(theme.NewStyles(theme.Unicode), theme.Unicode, 50, 6)
	if !strings.Contains(out, "GIMP") {
		t.Fatalf("the row is missing:\n%s", out)
	}
	if !strings.Contains(out, "3.2.8") {
		t.Errorf("the badge was dropped:\n%s", out)
	}
}
