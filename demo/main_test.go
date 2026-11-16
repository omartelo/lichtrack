package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omartelo/youtrack-lich/youtrack"
)

// TestScreensLoad reads the demo through the extension's own client, so a
// change to what the client asks for or decodes shows up here before it
// shows up as an empty screenshot.
func TestScreensLoad(t *testing.T) {
	srv := httptest.NewServer(handler())
	defer srv.Close()
	yt := youtrack.New(srv.URL, "demo")
	ctx := context.Background()

	boards, err := yt.Boards(ctx, youtrack.Page{})
	if err != nil || len(boards) == 0 {
		t.Fatalf("Boards = %d, %v", len(boards), err)
	}
	board, err := yt.SprintBoard(ctx, boards[0].ID, boards[0].CurrentSprint.ID)
	if err != nil {
		t.Fatalf("SprintBoard: %v", err)
	}
	if len(board.Unmatched) > 0 {
		t.Errorf("%d cards fit no column", len(board.Unmatched))
	}
	for _, c := range board.Columns {
		if len(c.Issues) == 0 {
			t.Errorf("column %q is empty", c.Column.Presentation)
		}
	}

	mine, err := yt.SearchIssues(ctx, "for: me #Unresolved", youtrack.Page{})
	if err != nil || len(mine) == 0 {
		t.Fatalf("SearchIssues = %d, %v", len(mine), err)
	}
	if saved, err := yt.SavedQueries(ctx, youtrack.Page{}); err != nil || len(saved) == 0 {
		t.Errorf("SavedQueries = %d, %v", len(saved), err)
	}

	const card = "ATL-142"
	issue, err := yt.Issue(ctx, card)
	if err != nil || issue.Description == "" {
		t.Fatalf("Issue = %+v, %v", issue, err)
	}
	if schema, err := yt.ProjectCustomFields(ctx, issue.Project.ID, youtrack.Page{}); err != nil || len(schema) != len(issue.CustomFields) {
		t.Errorf("ProjectCustomFields = %d fields, %v; the card has %d", len(schema), err, len(issue.CustomFields))
	}
	for name, n := range map[string]func() (int, error){
		"Links":       func() (int, error) { l, err := yt.Links(ctx, card); return len(l), err },
		"Attachments": func() (int, error) { a, err := yt.Attachments(ctx, card, youtrack.Page{}); return len(a), err },
		"Comments":    func() (int, error) { c, err := yt.Comments(ctx, card, youtrack.Page{}); return len(c), err },
		"WorkItems":   func() (int, error) { w, err := yt.WorkItems(ctx, card, youtrack.Page{}); return len(w), err },
		"WorkItemTypes": func() (int, error) {
			w, err := yt.WorkItemTypes(ctx, issue.Project.ID, youtrack.Page{})
			return len(w), err
		},
		"Activities": func() (int, error) {
			a, err := yt.Activities(ctx, card, []string{youtrack.CategoryCustomField}, youtrack.Page{})
			return len(a), err
		},
	} {
		if got, err := n(); err != nil || got == 0 {
			t.Errorf("%s = %d, %v", name, got, err)
		}
	}

	// The screenshot shows last week, which is whole whatever day it is.
	lastMonday := monday.AddDate(0, 0, -7)
	work, err := yt.WorkItemsBetween(ctx, youtrack.AuthorMe, lastMonday, lastMonday.AddDate(0, 0, 6), youtrack.Page{})
	if err != nil {
		t.Fatalf("WorkItemsBetween: %v", err)
	}
	days := map[string]bool{}
	for _, w := range work {
		days[time.UnixMilli(w.Date).Format(time.DateOnly)] = true
	}
	if len(days) != 5 {
		t.Errorf("last week has work on %d days, want the 5 weekdays", len(days))
	}
}
