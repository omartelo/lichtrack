package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lichdotdev/lich/extension"

	"github.com/omartelo/lichtrack/youtrack"
)

func TestParseDuration(t *testing.T) {
	valid := map[string]int{"1h 30m": 90, "2h": 120, "45m": 45, " 1H30M ": 90, "90m": 90}
	for typed, want := range valid {
		got, err := parseDuration(typed)
		if err != nil || got != want {
			t.Errorf("parseDuration(%q) = %d, %v; want %d", typed, got, err, want)
		}
	}
	for _, typed := range []string{"", "0m", "abc", "1.5h", "30", "1h 2h"} {
		if got, err := parseDuration(typed); err == nil {
			t.Errorf("parseDuration(%q) = %d; want an error", typed, got)
		}
	}
}

func TestFormatMinutes(t *testing.T) {
	for minutes, want := range map[int]string{45: "45m", 120: "2h", 330: "5h 30m", 0: "0m"} {
		if got := formatMinutes(minutes); got != want {
			t.Errorf("formatMinutes(%d) = %q; want %q", minutes, got, want)
		}
	}
}

func TestParseDay(t *testing.T) {
	// Late evening west of Greenwich: the UTC date is already the next day.
	now := time.Date(2026, 11, 12, 22, 30, 0, 0, time.FixedZone("BRT", -3*60*60))
	valid := map[string]string{
		"": "2026-11-12", "hoje": "2026-11-12", "Ontem": "2026-11-11",
		"2026-01-31": "2026-01-31", "31/01/2026": "2026-01-31",
	}
	for typed, want := range valid {
		got, err := parseDay(typed, now)
		if err != nil || got.Format(isoDay) != want || got.Location() != time.UTC || got.Hour() != 0 {
			t.Errorf("parseDay(%q) = %v, %v; want %s at midnight UTC", typed, got, err, want)
		}
		if shown := formatDay(got.UnixMilli()); err == nil && shown != got.Format(brDay) {
			t.Errorf("formatDay(parseDay(%q)) = %s; want %s", typed, shown, got.Format(brDay))
		}
	}
	for _, typed := range []string{"amanhã", "31/02/2026", "12/11"} {
		if got, err := parseDay(typed, now); err == nil {
			t.Errorf("parseDay(%q) = %v; want an error", typed, got)
		}
	}
}

func TestActivityValue(t *testing.T) {
	cases := map[string]string{
		`"Novo título"`:                          "Novo título",
		`{"name":"Em andamento"}`:                "Em andamento",
		`[{"fullName":"Ana"},{"login":"bruno"}]`: "Ana, bruno",
		`[{"idReadable":"LOJ-1","summary":"x"}]`: "LOJ-1",
		`null`:                                   "",
		`[]`:                                     "",
		`42`:                                     "42",
		`not json`:                               "",
	}
	for raw, want := range cases {
		if got := activityValue(json.RawMessage(raw)); got != want {
			t.Errorf("activityValue(%s) = %q; want %q", raw, got, want)
		}
	}
	long := make([]rune, activityValueLimit+10)
	for i := range long {
		long[i] = 'é'
	}
	raw, _ := json.Marshal(string(long))
	if got := []rune(activityValue(raw)); len(got) != activityValueLimit+1 {
		t.Errorf("a long value came back %d runes long; want %d and an ellipsis", len(got), activityValueLimit)
	}
}

func TestPickSprint(t *testing.T) {
	sprints := []youtrack.Sprint{{ID: "s1"}, {ID: "s2"}, {ID: "s3"}}
	current := youtrack.Agile{CurrentSprint: &youtrack.Sprint{ID: "s2"}}
	cases := []struct {
		name    string
		agile   youtrack.Agile
		sprints []youtrack.Sprint
		wanted  string
		want    string
	}{
		{"the one asked for", current, sprints, "s1", "s1"},
		{"the current one when none is asked", current, sprints, "", "s2"},
		{"the current one when the asked one is gone", current, sprints, "s9", "s2"},
		{"the last one without a current sprint", youtrack.Agile{}, sprints, "", "s3"},
		{"none on a board without sprints", youtrack.Agile{}, nil, "s1", ""},
	}
	for _, c := range cases {
		if got := pickSprint(c.agile, c.sprints, c.wanted); got != c.want {
			t.Errorf("%s: pickSprint = %q; want %q", c.name, got, c.want)
		}
	}
}

// unusedURL is the base of tests that never reach YouTrack.
const unusedURL = "http://youtrack.invalid"

func issue(id, summary, state string) youtrack.Issue {
	return youtrack.Issue{
		IDReadable: id, Summary: summary, Project: youtrack.Project{ID: "0-1"},
		CustomFields: []youtrack.IssueCustomField{
			{Name: "State", Value: json.RawMessage(`{"name":"` + state + `"}`)},
			{Name: "Assignee", Value: json.RawMessage(`{"login":"ana","fullName":"Ana M."}`)},
		},
	}
}

func boardApp(baseURL string, sent *[]extension.Message) *app {
	a := &app{
		done: make(chan func(), 8), query: defaultQuery,
		send: func(msg extension.Message) { *sent = append(*sent, msg) },
	}
	a.connect(youtrack.Config{BaseURL: baseURL, Token: "t"})
	a.board = youtrack.SprintBoard{
		Agile:  youtrack.Agile{ID: "b1", Name: "Loja"},
		Sprint: youtrack.Sprint{ID: "s1", Name: "Sprint 42"},
		Columns: []youtrack.BoardColumn{
			{Column: youtrack.AgileColumn{Presentation: "Aberto"}, Issues: []youtrack.Issue{
				issue("LOJ-1", "Primeiro", "Aberto"), issue("LOJ-2", "Segundo", "Aberto"),
			}},
			{Column: youtrack.AgileColumn{Presentation: "Feito"}, Issues: []youtrack.Issue{issue("LOJ-3", "Terceiro", "Feito")}},
		},
	}
	a.boards, a.boardID, a.sprintID = []youtrack.Agile{a.board.Agile}, "b1", "s1"
	a.sprints = []youtrack.Sprint{a.board.Sprint}
	return a
}

func ids(issues []youtrack.Issue) []string {
	out := make([]string, len(issues))
	for i, issue := range issues {
		out[i] = issue.IDReadable
	}
	return out
}

func TestMoveCardMovesOnScreenBeforeYouTrackAnswers(t *testing.T) {
	var sent []extension.Message
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"forbidden","error_description":"sem permissão"}`, http.StatusForbidden)
	}))
	defer refused.Close()
	a := boardApp(refused.URL, &sent)

	a.event(extension.EventData{Handler: on(hMove, 1), Key: "LOJ-1", Index: 0, From: columnKey(0)})

	if got := ids(a.board.Columns[0].Issues); len(got) != 1 || got[0] != "LOJ-2" {
		t.Errorf("the column left holds %v; want [LOJ-2]", got)
	}
	if got := ids(a.board.Columns[1].Issues); len(got) != 2 || got[0] != "LOJ-1" || got[1] != "LOJ-3" {
		t.Errorf("the column entered holds %v; want [LOJ-1 LOJ-3]", got)
	}

	(<-a.done)()
	if len(sent) != 1 || sent[0].Data.(extension.ToastData).Variant != extension.ToastError {
		t.Fatalf("a refused move sent %+v; want one error toast", sent)
	}
	// The board is fetched again, which is what puts the card back.
	if a.busy != 1 {
		t.Errorf("%d requests in flight after a refused move; want the board's reload", a.busy)
	}
}

func TestMoveCardIgnoresWhatItCannotPlace(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	events := []extension.EventData{
		{Handler: on(hMove, 1), Key: "LOJ-9", From: columnKey(0)},
		{Handler: on(hMove, 7), Key: "LOJ-1", From: columnKey(0)},
		{Handler: on(hMove, 1), Key: "LOJ-1", From: "elsewhere"},
		{Handler: on(hMove, 0), Key: "LOJ-1", From: columnKey(0)},
	}
	for _, e := range events {
		a.event(e)
		if len(a.board.Columns[0].Issues) != 2 || len(a.board.Columns[1].Issues) != 1 || a.busy != 0 {
			t.Fatalf("%+v moved a card or started a request", e)
		}
	}
}

// Every screen has to survive the trip to lich: Message.UnmarshalJSON decodes
// each node's props into the type its component declares and refuses an
// unknown component.
func TestEveryScreenIsATreeLichReads(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	a.issues = []youtrack.Issue{issue("LOJ-1", "Primeiro", "Aberto")}
	a.saved = []youtrack.SavedQuery{{Name: "Meus", Query: defaultQuery}}
	a.more = true
	a.lichProjects = []extension.Project{{Name: "api", Path: "/p/api"}, {Name: "web", Path: "/p/web"}}
	schema := []youtrack.ProjectCustomField{{CanBeEmpty: false, Field: youtrack.CustomField{
		Name: "State", FieldType: youtrack.FieldType{ID: "state[1]"},
	}}}
	schema[0].Bundle = &struct {
		ID     string                 `json:"id"`
		Values []youtrack.BundleValue `json:"values"`
		Users  []youtrack.User        `json:"aggregatedUsers"`
	}{Values: []youtrack.BundleValue{{Name: "Aberto"}, {Name: "Feito"}}}
	updated := int64(1)
	open := &detail{
		issue: issue("LOJ-1", "Primeiro", "Aberto"), schema: schema, workType: noOption,
		links: []youtrack.IssueLink{{Direction: "INWARD", LinkType: youtrack.LinkType{TargetToSource: "depende de"},
			Issues: []youtrack.Issue{issue("LOJ-3", "Terceiro", "Feito")}}},
		attachments: []youtrack.Attachment{{Name: "tela.png", URL: "/api/files/1"}},
		comments:    []youtrack.Comment{{Text: "oi", Created: 1, Updated: &updated, Author: &youtrack.User{Login: "ana"}}},
		work:        []youtrack.WorkItem{{Date: 1, Duration: youtrack.Duration{Minutes: 90}, Type: &youtrack.WorkItemType{Name: "Dev"}}},
		workTypes:   []youtrack.WorkItemType{{ID: "1", Name: "Dev"}},
		activities:  []youtrack.ActivityItem{{Timestamp: 1, Added: json.RawMessage(`"x"`)}},
	}
	open.issue.Tags = []youtrack.Tag{{Name: "cadastro"}}

	screens := map[string]func(){
		"not configured": func() { a.yt = nil },
		"board":          func() {},
		"empty board":    func() { a.board = youtrack.SprintBoard{} },
		"issues":         func() { a.tab = tabIssues },
		"timesheet": func() {
			a.tab, a.sheet = tabSheet, timesheet{week: weekOf(time.Now()), items: []youtrack.WorkItem{logged("LOJ-1", time.Now().UTC().Format(isoDay), 90)}}
		},
		"no issues":        func() { a.tab, a.issues = tabIssues, nil },
		"description":      func() { a.sel = open },
		"editing":          func() { a.sel, open.editing = open, true },
		"preview":          func() { a.sel, open.editing, open.preview = open, true, true },
		"comments":         func() { a.sel, open.tab = open, detailComments },
		"work":             func() { a.sel, open.tab = open, detailWork },
		"history":          func() { a.sel, open.tab = open, detailHistory },
		"create view":      func() { a.create = createForm{open: true, schema: schema, values: map[string]string{"State": "Feito"}} },
		"config dialog":    func() { a.config = configForm{open: true} },
		"one lich project": func() { a.sel, a.lichProjects = open, a.lichProjects[:1] },
	}
	for name, arrange := range screens {
		saved, savedOpen := *a, *open
		arrange()
		sent = nil
		a.render()
		*a, *open = saved, savedOpen

		raw, err := json.Marshal(sent[0])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var back extension.Message
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Errorf("%s: lich would refuse this tree: %v", name, err)
		}
	}
}

func TestToggleCardFieldIsKeptAcrossRuns(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	a.dataDir = t.TempDir()

	a.event(extension.EventData{Handler: on(hCardField, "State"), Checked: true})
	a.event(extension.EventData{Handler: on(hCardField, "Tipo: detalhe"), Checked: true})
	a.event(extension.EventData{Handler: on(hCardField, "State")})

	kept, err := LoadSettings(a.dataDir)
	if err != nil || len(kept.CardFields) != 1 || kept.CardFields[0] != "Tipo: detalhe" {
		t.Fatalf("settings hold %v, %v; want only the field still chosen", kept.CardFields, err)
	}
	if len(sent) != 0 {
		t.Errorf("toggling sent %+v; want no toast", sent)
	}
}

func TestCardShowsTheChosenFieldsThatHaveAValue(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	a.cardFields = []string{"State", "Estimativa"}

	raw, err := json.Marshal(a.cardFace(issue("LOJ-1", "Primeiro", "Aberto")))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); !strings.Contains(got, `"type":"tag","props":{"text":"Aberto"}`) || strings.Count(got, `"type":"tag"`) != 1 {
		t.Errorf("the card is %s; want one tag, with the state", got)
	}
}

func TestBoardFieldsListsEachFieldOnce(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	if got := a.boardFields(); len(got) != 2 || got[0] != "State" || got[1] != "Assignee" {
		t.Errorf("boardFields = %v; want [State Assignee]", got)
	}
}

func TestAvatarsAreFetchedOnceAndDrawnOnTheCard(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 32)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(png))
	}))
	defer srv.Close()
	var sent []extension.Message
	a := boardApp(srv.URL, &sent)
	withPicture := issue("LOJ-1", "Primeiro", "Aberto")
	withPicture.CustomFields[1].Value = json.RawMessage(`{"login":"ana","fullName":"Ana M.","avatarUrl":"/hub/avatar/ana"}`)
	a.board.Columns[0].Issues = []youtrack.Issue{withPicture, withPicture}

	a.loadAvatars()
	a.loadAvatars()
	(<-a.done)()
	a.loadAvatars()

	if requests != 1 || a.busy != 0 {
		t.Fatalf("%d requests, %d in flight; want the picture fetched once", requests, a.busy)
	}
	raw, err := json.Marshal(a.cardFace(withPicture))
	if err != nil {
		t.Fatal(err)
	}
	var face extension.Node
	if err := json.Unmarshal(raw, &face); err != nil {
		t.Fatal(err)
	}
	picture := face.Children[0].Children[1].Children[0].Props.(extension.AvatarProps)
	if string(picture.Image) != png || picture.Initial != "A" {
		t.Errorf("the card's avatar is %+v; want the picture and the initial", picture)
	}
}

func manyBoards(n int) []youtrack.Agile {
	boards := make([]youtrack.Agile, n)
	for i := range boards {
		boards[i] = youtrack.Agile{ID: "b" + strconv.Itoa(i), Name: "Board " + strconv.Itoa(i)}
	}
	return boards
}

func TestLongSelectFiltersByWhatWasTyped(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	a.boards = append(manyBoards(menuMaxEntries), youtrack.Agile{ID: "sop", Name: "SOP Integração"})

	a.event(extension.EventData{Handler: on(hPickFilter, hBoard), Text: " integra "})

	raw, err := json.Marshal(a.boardSelect())
	if err != nil {
		t.Fatal(err)
	}
	var panel extension.Node
	if err := json.Unmarshal(raw, &panel); err != nil {
		t.Fatalf("lich would refuse the select: %v", err)
	}
	if panel.Type != extension.DropdownPanel.Name {
		t.Fatalf("a long list drew a %s; want a panel", panel.Type)
	}
	if got := string(raw); strings.Count(got, `"type":"navRow"`) != 1 || !strings.Contains(got, `"onPress":"pick:board:sop"`) {
		t.Errorf("the filtered select is %s; want only the board that matches", got)
	}
}

func TestShortSelectStaysAMenu(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	a.boards = manyBoards(menuMaxEntries)
	if got := a.boardSelect().Type; got != extension.DropdownMenu.Name {
		t.Errorf("a short list drew a %s; want a menu", got)
	}
}

func TestPickingARowRunsItsHandlerAndShutsThePanel(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	a.dataDir = t.TempDir()
	a.pickFilters = map[string]string{"x": "y"}
	before := a.pickEpoch

	a.event(extension.EventData{Handler: on(hPick, on(hCardField, "Tipo: detalhe"))})

	if len(a.cardFields) != 1 || a.cardFields[0] != "Tipo: detalhe" {
		t.Errorf("the row's handler left cardFields %v; want it run with its whole argument", a.cardFields)
	}
	if a.pickEpoch == before || a.pickFilters != nil {
		t.Errorf("epoch %d, filters %v; want a new panel key and no filter", a.pickEpoch, a.pickFilters)
	}
}

func TestCreateFieldCanBeChosenAndLeftToTheProject(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	state := youtrack.ProjectCustomField{CanBeEmpty: true, Field: youtrack.CustomField{
		Name: "State", FieldType: youtrack.FieldType{ID: "state[1]"},
	}}
	state.Bundle = &struct {
		ID     string                 `json:"id"`
		Values []youtrack.BundleValue `json:"values"`
		Users  []youtrack.User        `json:"aggregatedUsers"`
	}{Values: []youtrack.BundleValue{{Name: "Aberto"}, {Name: "Feito"}}}
	a.create = createForm{open: true, schema: []youtrack.ProjectCustomField{state}, values: map[string]string{}}

	a.event(extension.EventData{Handler: on(hNewField, 0, 1)})
	if got := a.create.values["State"]; got != "Feito" {
		t.Fatalf("choosing the second option left %q; want Feito", got)
	}
	a.event(extension.EventData{Handler: on(hNewField, 0, 9)})
	if got := a.create.values["State"]; got != "Feito" {
		t.Errorf("an option that does not exist left %q; want Feito kept", got)
	}
	a.event(extension.EventData{Handler: on(hNewField, 0, noOption)})
	if _, chosen := a.create.values["State"]; chosen {
		t.Error("the empty option kept a value; want the field left to the project")
	}
}

func TestEditSavesTheTitleAndTheDescriptionTogether(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(raw))
		}
		_, _ = io.WriteString(w, `{"idReadable":"LOJ-1","summary":"Novo","description":"nova"}`)
	}))
	defer srv.Close()
	var sent []extension.Message
	a := boardApp(srv.URL, &sent)
	a.sel = &detail{issue: issue("LOJ-1", "Primeiro", "Aberto"), tab: detailHistory}

	a.event(extension.EventData{Handler: hEdit})
	if d := a.sel; !d.editing || d.tab != detailDescription || d.summaryDraft != "Primeiro" {
		t.Fatalf("the pencil left %+v; want the editor open on the description with the title in it", d)
	}
	a.event(extension.EventData{Handler: hSummaryDraft, Text: "  "})
	a.event(extension.EventData{Handler: hEditSave})
	if !a.sel.editing || a.busy != 0 || len(sent) != 1 {
		t.Fatalf("an empty title: editing %v, %d requests, %d toasts; want the editor kept and one error", a.sel.editing, a.busy, len(sent))
	}

	a.event(extension.EventData{Handler: hSummaryDraft, Text: " Novo "})
	a.event(extension.EventData{Handler: hDescDraft, Text: "nova"})
	a.event(extension.EventData{Handler: hEditSave})
	(<-a.done)()

	if len(bodies) != 2 || !strings.Contains(bodies[0], `"summary":"Novo"`) || !strings.Contains(bodies[1], `"description":"nova"`) {
		t.Errorf("YouTrack got %v; want the title, then the description", bodies)
	}
	if a.sel.editing || a.sel.issue.Summary != "Novo" {
		t.Errorf("after saving: editing %v, title %q; want the editor shut on the new card", a.sel.editing, a.sel.issue.Summary)
	}
}

func TestWeekOfIsTheMondayAtMidnightUTC(t *testing.T) {
	brt := time.FixedZone("BRT", -3*60*60)
	for typed, want := range map[string]string{
		"2026-11-12 22:30": "2026-11-09", // a Thursday
		"2026-11-09 00:10": "2026-11-09", // the Monday itself
		"2026-11-15 23:50": "2026-11-09", // the Sunday that ends it
	} {
		at, _ := time.ParseInLocation("2006-01-02 15:04", typed, brt)
		got := weekOf(at)
		if got.Format(isoDay) != want || got.Location() != time.UTC || got.Hour() != 0 {
			t.Errorf("weekOf(%s) = %v; want %s at midnight UTC", typed, got, want)
		}
	}
}

func logged(id, day string, minutes int) youtrack.WorkItem {
	at, _ := time.Parse(isoDay, day)
	w := youtrack.WorkItem{Date: at.UnixMilli(), Duration: youtrack.Duration{Minutes: minutes}}
	if id != "" {
		w.Issue = &struct {
			IDReadable string `json:"idReadable"`
			Summary    string `json:"summary"`
		}{IDReadable: id, Summary: "card " + id}
	}
	return w
}

func TestSheetRowsAddsUpEachIssueByDay(t *testing.T) {
	monday, _ := time.Parse(isoDay, "2026-11-09")
	rows, totals := sheetRows([]youtrack.WorkItem{
		logged("LOJ-2", "2026-11-09", 60), logged("LOJ-1", "2026-11-09", 30), logged("LOJ-1", "2026-11-09", 15),
		logged("LOJ-1", "2026-11-15", 120),
		logged("LOJ-1", "2026-11-08", 500), logged("LOJ-1", "2026-11-16", 500), logged("", "2026-11-10", 500),
	}, monday)

	if len(rows) != 2 || rows[0].id != "LOJ-1" || rows[1].id != "LOJ-2" {
		t.Fatalf("rows = %+v; want LOJ-1 then LOJ-2", rows)
	}
	if rows[0].days[0] != 45 || rows[0].days[6] != 120 || rows[0].total != 165 || rows[1].total != 60 {
		t.Errorf("rows = %+v; want LOJ-1 with 45 on Monday and 120 on Sunday", rows)
	}
	if totals != [daysInWeek]int{105, 0, 0, 0, 0, 0, 120} {
		t.Errorf("totals = %v; want what fell outside the week and off an issue left out", totals)
	}
}

func bundled(name, kind string, values ...string) youtrack.ProjectCustomField {
	f := youtrack.ProjectCustomField{CanBeEmpty: true, Field: youtrack.CustomField{Name: name, FieldType: youtrack.FieldType{ID: kind}}}
	if len(values) == 0 {
		return f
	}
	f.Bundle = &struct {
		ID     string                 `json:"id"`
		Values []youtrack.BundleValue `json:"values"`
		Users  []youtrack.User        `json:"aggregatedUsers"`
	}{}
	for _, v := range values {
		f.Bundle.Values = append(f.Bundle.Values, youtrack.BundleValue{Name: v})
	}
	return f
}

func TestCreateFieldsCarriesWhatTheFormChose(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	a.create = createForm{open: true, values: map[string]string{}, multi: map[string][]string{}, schema: []youtrack.ProjectCustomField{
		bundled("State", "state[1]", "Aberto", "Feito"), bundled("Sprint", "enum[*]", "S1", "S2", "S3"),
		bundled("Estimativa", "period"), bundled("Módulo", "enum[1]", "TMS"),
	}}
	a.event(extension.EventData{Handler: on(hNewField, 0, 1)})
	a.event(extension.EventData{Handler: on(hNewFieldToggle, 1, 0)})
	a.event(extension.EventData{Handler: on(hNewFieldToggle, 1, 2)})
	a.event(extension.EventData{Handler: on(hNewFieldToggle, 1, 0)})
	a.event(extension.EventData{Handler: on(hNewFieldText, 2), Text: "4h 30m"})

	fields, err := createFields(a.create)
	raw, _ := json.Marshal(fields)
	want := `[{"name":"State","$type":"StateIssueCustomField","value":{"name":"Feito"}},` +
		`{"name":"Sprint","$type":"MultiEnumIssueCustomField","value":[{"name":"S3"}]},` +
		`{"name":"Estimativa","$type":"PeriodIssueCustomField","value":{"minutes":270}}]`
	if err != nil || string(raw) != want {
		t.Errorf("createFields = %s, %v\nwant %s", raw, err, want)
	}

	a.event(extension.EventData{Handler: on(hNewFieldText, 2), Text: "amanhã"})
	if _, err := createFields(a.create); err == nil || !strings.Contains(err.Error(), "Estimativa") {
		t.Errorf("a period that does not parse answered %v; want an error naming the field", err)
	}
}

func TestCardFieldsOfSeveralValuesAndPeriodsAreSaved(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(raw))
		}
		if strings.HasSuffix(r.URL.Path, "/agiles") {
			// The reload that follows a save: no board, so nothing to toast.
			_, _ = io.WriteString(w, `[]`)
			return
		}
		_, _ = io.WriteString(w, `{"idReadable":"LOJ-1"}`)
	}))
	defer srv.Close()
	var sent []extension.Message
	a := boardApp(srv.URL, &sent)
	card := issue("LOJ-1", "Primeiro", "Aberto")
	card.CustomFields = append(card.CustomFields, youtrack.IssueCustomField{Name: "Sprint", Value: json.RawMessage(`[{"name":"S1"}]`)})
	a.sel = &detail{issue: card, schema: []youtrack.ProjectCustomField{bundled("Sprint", "enum[*]", "S1", "S2"), bundled("Estimativa", "period")}}

	settle := func() {
		for a.busy > 0 {
			(<-a.done)()
		}
		a.sel.issue = card
	}
	a.event(extension.EventData{Handler: on(hFieldToggle, 0, 1)})
	settle()
	a.event(extension.EventData{Handler: on(hFieldText, 1), Text: "2h"})
	settle()
	a.event(extension.EventData{Handler: on(hFieldText, 1), Text: "dois dias"})

	if len(bodies) != 2 || !strings.Contains(bodies[0], `"value":[{"name":"S1"},{"name":"S2"}]`) || !strings.Contains(bodies[1], `"value":{"minutes":120}`) {
		t.Errorf("YouTrack got %v; want the sprint added to the one there, then the estimate", bodies)
	}
	if n := len(sent); n != 1 || sent[0].Data.(extension.ToastData).Variant != extension.ToastError {
		t.Errorf("%d messages after a period that does not parse; want one error toast and no request", n)
	}
}

func TestTickReloadsOnlyTheBoardOnScreen(t *testing.T) {
	var sent []extension.Message
	a := boardApp(unusedURL, &sent)
	idle := map[string]func(){
		"surface hidden": func() {},
		"issues tab":     func() { a.visible, a.tab = true, tabIssues },
		"a card open":    func() { a.visible, a.sel = true, &detail{} },
		"creating":       func() { a.visible, a.create.open = true, true },
		"a request out":  func() { a.visible, a.busy = true, 1 },
	}
	for name, arrange := range idle {
		saved := *a
		arrange()
		before := a.busy
		a.tick()
		if a.busy != before {
			t.Errorf("%s: tick started a request", name)
		}
		*a = saved
	}
	a.visible = true
	a.tick()
	if a.busy != 1 {
		t.Errorf("%d requests with the board on screen; want its reload", a.busy)
	}
}

func TestSortedQueryKeepsAnOrderTheQueryNames(t *testing.T) {
	cases := []struct {
		query string
		order int
		want  string
	}{
		{"for: me", 0, "for: me"},
		{"for: me", 1, "for: me sort by: updated desc"},
		{"", 2, "sort by: created desc"},
		{"for: me Sort By: priority", 1, "for: me Sort By: priority"},
		{"for: me", 99, "for: me"},
		{"for: me", -1, "for: me"},
	}
	for _, c := range cases {
		if got := sortedQuery(c.query, c.order); got != c.want {
			t.Errorf("sortedQuery(%q, %d) = %q; want %q", c.query, c.order, got, c.want)
		}
	}
}

func TestChosenBoardIsKeptAndStarredBoardsLead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/agiles") {
			_, _ = io.WriteString(w, `[{"id":"b1","name":"Um"},{"id":"b2","name":"Dois","favorite":true},{"id":"b3","name":"Três","favorite":true}]`)
			return
		}
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	var sent []extension.Message
	a := boardApp(srv.URL, &sent)
	a.dataDir = t.TempDir()

	a.loadBoard("", "")
	(<-a.done)()
	if a.boardID != "b2" || a.boards[0].ID != "b2" || a.boards[2].ID != "b1" {
		t.Fatalf("with no board chosen: showing %q, menu %v; want the first starred board, starred ones first", a.boardID, a.boards)
	}

	a.event(extension.EventData{Handler: on(hBoard, "b1")})
	(<-a.done)()
	kept, err := LoadSettings(a.dataDir)
	if err != nil || kept.Board != "b1" || a.boardID != "b1" {
		t.Errorf("after choosing b1: settings %+v, %v, showing %q; want b1 kept", kept, err, a.boardID)
	}
	if len(sent) != 0 {
		t.Errorf("choosing a board sent %+v; want no toast", sent)
	}
}
