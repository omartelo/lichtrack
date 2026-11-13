package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lichdotdev/lich/extension"

	"github.com/omartelo/lichtrack/youtrack"
)

const (
	surfaceID  = "main"
	searchKey  = "search"
	summaryKey = "new.summary"
	urlKey     = "config.url"

	callTimeout  = 60 * time.Second
	defaultQuery = "for: me #Unresolved"
	// One request has to bring every sprint, project and field the menus list.
	menuPageSize = 200
	// lich caps a prompt at 8 KB (extension.OpenSessionParams); the rest of it
	// is the card's id, title and link.
	promptDescriptionLimit = 6000

	tabBoard  = 0
	tabIssues = 1
	tabSheet  = 2

	sheetDurationKey = "sheet.duration"
	sheetPrev        = "prev"
	sheetNext        = "next"
	sheetNow         = "now"

	detailDescription = 0
	detailComments    = 1
	detailWork        = 2
	detailHistory     = 3

	noOption = -1
)

// Handler names. One that carries an argument is spelled name:argument, see on.
const (
	hTab            = "tab"
	hQuery          = "query"
	hSearch         = "search"
	hFind           = "find"
	hReload         = "reload"
	hSort           = "sort"
	hMore           = "more"
	hBoard          = "board"
	hSprint         = "sprint"
	hSaved          = "saved"
	hOpen           = "open"
	hPick           = "pick"
	hPickFilter     = "pick.filter"
	hCardField      = "card.field"
	hMove           = "move"
	hClose          = "close"
	hLink           = "link"
	hEdit           = "edit"
	hEditSave       = "edit.save"
	hEditCancel     = "edit.cancel"
	hSummaryDraft   = "summary.draft"
	hDetailTab      = "detail.tab"
	hExternal       = "external"
	hField          = "field"
	hSession        = "session"
	hAttachment     = "attachment"
	hDescDraft      = "desc.draft"
	hDescPreview    = "desc.preview"
	hCommentDraft   = "comment.draft"
	hCommentSend    = "comment.send"
	hWorkDate       = "work.date"
	hWorkDuration   = "work.duration"
	hWorkText       = "work.text"
	hWorkType       = "work.type"
	hWorkSend       = "work.send"
	hNew            = "new"
	hNewProject     = "new.project"
	hNewSummary     = "new.summary"
	hNewDesc        = "new.desc"
	hNewField       = "new.field"
	hFieldToggle    = "field.toggle"
	hFieldText      = "field.text"
	hNewFieldToggle = "new.field.toggle"
	hNewFieldText   = "new.field.text"
	hSheetWeek      = "sheet.week"
	hSheetCell      = "sheet.cell"
	hSheetIssue     = "sheet.issue"
	hSheetDate      = "sheet.date"
	hSheetDuration  = "sheet.duration"
	hSheetText      = "sheet.text"
	hSheetSend      = "sheet.send"
	hNewCreate      = "new.create"
	hNewCancel      = "new.cancel"
	hConfig         = "config"
	hConfigURL      = "config.url"
	hConfigToken    = "config.token"
	hConfigInsecure = "config.insecure"
	hConfigSave     = "config.save"
	hConfigCancel   = "config.cancel"

	argSeparator = ":"
)

// descriptionTemplate is the team's card standard (contexto, estória,
// comportamento esperado, critérios de aceite).
const descriptionTemplate = "## Contexto\n\n\n## Estória\n\nComo , quero , para .\n\n" +
	"## Comportamento esperado\n\n- \n\n## Critérios de aceite\n\n- \n"

var historyCategories = []string{
	youtrack.CategoryIssueCreated, youtrack.CategoryCustomField, youtrack.CategorySummary,
	youtrack.CategoryDescription, youtrack.CategoryResolved, youtrack.CategorySprint,
	youtrack.CategoryTags, youtrack.CategoryLinks,
}

// singleValueFields are the field types a select can edit: one value out of
// a set the project defines.
var singleValueFields = map[string]func(field, value string) youtrack.CustomFieldUpdate{
	"enum[1]":       youtrack.EnumField,
	"state[1]":      youtrack.StateField,
	"version[1]":    youtrack.VersionField,
	"build[1]":      youtrack.BuildField,
	"ownedField[1]": youtrack.OwnedField,
	"user[1]":       youtrack.UserField,
}

func on(name string, args ...any) extension.Handler {
	parts := []string{name}
	for _, arg := range args {
		parts = append(parts, fmt.Sprint(arg))
	}
	return extension.Handler(strings.Join(parts, argSeparator))
}

// app is the whole state of the screen. Only the goroutine running main's
// loop touches it: a request to YouTrack runs on its own goroutine and hands
// back, through done, the function that applies its result.
type app struct {
	send    func(extension.Message)
	conn    *extension.Conn
	done    chan func()
	dataDir string

	cfg youtrack.Config
	// yt is nil until a connection is configured.
	yt   *youtrack.Client
	busy int

	tab      int
	boards   []youtrack.Agile
	boardID  string
	sprints  []youtrack.Sprint
	sprintID string
	board    youtrack.SprintBoard

	query  string
	saved  []youtrack.SavedQuery
	issues []youtrack.Issue
	more   bool
	// sort indexes sortOrders.
	sort int

	sel *detail

	// pickFilters is what was typed into each long select's filter, by the
	// select's id; pickEpoch counts the choices made in one, see choice.
	pickFilters map[string]string
	pickEpoch   int

	sheet timesheet
	// visible is whether the surface is on screen, which is when tick reloads.
	visible bool

	// cardFields names the custom fields a board card shows.
	cardFields []string
	// avatars is each picture by its url; an entry without bytes was asked
	// for already, and is not asked for again.
	avatars map[string][]byte

	config       configForm
	create       createForm
	lichProjects []extension.Project

	// focus is the key of the field to hand the keyboard once the next tree
	// is out: lich drops a focus for a field it has not drawn yet.
	focus string
}

type detail struct {
	issue       youtrack.Issue
	schema      []youtrack.ProjectCustomField
	links       []youtrack.IssueLink
	attachments []youtrack.Attachment
	comments    []youtrack.Comment
	work        []youtrack.WorkItem
	workTypes   []youtrack.WorkItemType
	activities  []youtrack.ActivityItem

	tab          int
	editing      bool
	preview      bool
	summaryDraft string
	descDraft    string
	commentDraft string
	workDate     string
	workDuration string
	workText     string
	workType     int
}

type configForm struct {
	open     bool
	url      string
	token    string
	insecure bool
}

type createForm struct {
	open     bool
	projects []youtrack.Project
	project  int
	summary  string
	desc     string
	schema   []youtrack.ProjectCustomField
	// values is the option chosen for each required field, by field name.
	values map[string]string
	// multi is the options chosen for each field that holds several.
	multi map[string][]string
}

type option struct {
	value string
	label string
}

func (a *app) connect(cfg youtrack.Config) {
	a.cfg = cfg
	a.yt = cfg.Client()
}

func (a *app) handle(msg extension.Message) {
	switch d := msg.Data.(type) {
	case extension.SurfaceData:
		a.visible = msg.Kind == extension.SurfaceOpened.Name
		if a.visible {
			a.loadLichProjects()
			a.loadSaved()
			a.reload()
			a.render()
		}
	case extension.EventData:
		a.event(d)
		a.render()
		if a.focus != "" {
			a.send(extension.Focus.New(extension.FocusData{Surface: surfaceID, Key: a.focus}))
			a.focus = ""
		}
	}
}

// bg runs work off the loop. work must not touch the app: it returns the
// function that does, which the loop runs.
func (a *app) bg(failure string, work func(context.Context) (func(), error)) {
	a.busy++
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		apply, err := work(ctx)
		a.done <- func() {
			a.busy--
			if err != nil {
				a.fail(failure, err)
				return
			}
			apply()
		}
	}()
}

func (a *app) fail(title string, err error) {
	log.Printf("%s: %v", title, err)
	a.send(extension.ShowToast.New(extension.ToastData{
		Variant: extension.ToastError, Title: title, Description: err.Error(),
	}))
}

func (a *app) toast(title string) {
	a.send(extension.ShowToast.New(extension.ToastData{Variant: extension.ToastSuccess, Title: title}))
}

func (a *app) event(e extension.EventData) {
	name, arg, _ := strings.Cut(string(e.Handler), argSeparator)
	switch name {
	case hPick:
		// arg is the handler of the row chosen.
		a.pickEpoch++
		a.pickFilters = nil
		name, arg, _ = strings.Cut(arg, argSeparator)
	case hPickFilter:
		a.pickFilters = map[string]string{arg: e.Text}
		return
	}
	if a.sel != nil && a.detailEvent(a.sel, name, arg, e) {
		return
	}
	if a.formEvent(name, arg, e) || a.sheetEvent(name, arg, e) {
		return
	}
	switch name {
	case hTab:
		a.sel, a.create = nil, createForm{}
		a.tab = e.Index
		if arg != "" {
			a.tab, _ = strconv.Atoi(arg)
		}
		a.reload()
	case hQuery:
		a.query = e.Text
	case hSearch:
		a.tab = tabIssues
		a.search(0)
	case hSaved:
		if i, err := strconv.Atoi(arg); err == nil && i < len(a.saved) {
			a.query = a.saved[i].Query
			a.search(0)
		}
	case hSort:
		if i, err := strconv.Atoi(arg); err == nil && i >= 0 && i < len(sortOrders) {
			a.sort = i
			a.search(0)
		}
	case hMore:
		a.search(len(a.issues))
	case hFind:
		a.focus = searchKey
	case hReload:
		a.reload()
		if a.sel != nil {
			a.loadDetail(a.sel.issue.IDReadable)
		}
	case hBoard:
		a.boardID = arg
		a.saveSettings()
		a.loadBoard(arg, "")
	case hSprint:
		a.loadBoard(a.boardID, arg)
	case hMove:
		a.moveCard(e, arg)
	case hCardField:
		a.toggleCardField(arg)
	case hOpen:
		a.open(arg)
	case hExternal:
		a.send(extension.OpenExternal.New(extension.OpenExternalData{URL: a.issueURL(arg)}))
	case hLink:
		// lich opens only http and https, and drops the rest.
		a.send(extension.OpenExternal.New(extension.OpenExternalData{URL: e.Text}))
	}
}

func (a *app) formEvent(name, arg string, e extension.EventData) bool {
	switch name {
	case hNew:
		a.openCreate()
	case hNewProject:
		if i, err := strconv.Atoi(arg); err == nil && i < len(a.create.projects) {
			a.create.project = i
			a.loadCreateSchema()
		}
	case hNewSummary:
		a.create.summary = e.Text
	case hNewDesc:
		a.create.desc = e.Text
	case hNewField:
		a.chooseCreateField(arg)
	case hNewFieldToggle:
		a.toggleCreateField(arg)
	case hNewFieldText:
		a.typeCreateField(arg, e.Text)
	case hNewCreate:
		a.createIssue()
	case hNewCancel:
		a.create = createForm{}
	case hConfig:
		a.config = configForm{open: true, url: a.cfg.BaseURL, token: a.cfg.Token, insecure: a.cfg.Insecure}
		a.focus = urlKey
	case hConfigURL:
		a.config.url = e.Text
	case hConfigToken:
		a.config.token = e.Text
	case hConfigInsecure:
		a.config.insecure = e.Checked
	case hConfigSave:
		a.saveConfig()
	case hConfigCancel:
		a.config = configForm{}
	default:
		return false
	}
	return true
}

func (a *app) detailEvent(d *detail, name, arg string, e extension.EventData) bool {
	switch name {
	case hClose:
		a.sel = nil
	case hEdit:
		d.editing, d.preview, d.tab = true, false, detailDescription
		d.summaryDraft, d.descDraft = d.issue.Summary, d.issue.Description
	case hSummaryDraft:
		d.summaryDraft = e.Text
	case hDetailTab:
		d.tab = e.Index
		a.loadDetailTab(d)
	case hAttachment:
		if i, err := strconv.Atoi(arg); err == nil && i < len(d.attachments) {
			a.send(extension.OpenExternal.New(extension.OpenExternalData{URL: a.cfg.BaseURL + d.attachments[i].URL}))
		}
	case hField:
		a.setField(d, arg)
	case hFieldToggle:
		a.toggleField(d, arg)
	case hFieldText:
		a.setPeriod(d, arg, e.Text)
	case hSession:
		a.openSession(d, arg)
	case hDescDraft:
		d.descDraft = e.Text
	case hDescPreview:
		d.preview = !d.preview
	case hEditCancel:
		d.editing = false
	case hEditSave:
		a.saveEdit(d)
	case hCommentDraft:
		d.commentDraft = e.Text
	case hCommentSend:
		a.sendComment(d)
	case hWorkDate:
		d.workDate = e.Text
	case hWorkDuration:
		d.workDuration = e.Text
	case hWorkText:
		d.workText = e.Text
	case hWorkType:
		if i, err := strconv.Atoi(arg); err == nil && i < len(d.workTypes) {
			d.workType = i
		}
	case hWorkSend:
		a.sendWork(d)
	default:
		return false
	}
	return true
}

// reload fetches again the list on screen.
func (a *app) reload() {
	if a.yt == nil {
		return
	}
	switch a.tab {
	case tabIssues:
		a.search(0)
	case tabSheet:
		a.loadTimesheet()
	default:
		a.loadBoard(a.boardID, a.sprintID)
	}
}

// loadBoard shows boardID and sprintID; an empty or unknown one falls back to
// the first board and to its current sprint.
func (a *app) loadBoard(boardID, sprintID string) {
	yt := a.yt
	a.bg("Não foi possível carregar o board", func(ctx context.Context) (func(), error) {
		boards, err := yt.Boards(ctx, youtrack.Page{Top: menuPageSize})
		if err != nil {
			return nil, err
		}
		if len(boards) == 0 {
			return func() { a.boards, a.sprints, a.board = nil, nil, youtrack.SprintBoard{} }, nil
		}
		// The starred boards lead the menu, and the first of them is the one
		// shown until the person chooses another.
		slices.SortStableFunc(boards, func(a, b youtrack.Agile) int {
			return cmp.Compare(boolRank(b.Favorite), boolRank(a.Favorite))
		})
		agile := boards[0]
		for _, b := range boards {
			if b.ID == boardID {
				agile = b
			}
		}
		sprints, err := yt.Sprints(ctx, agile.ID, youtrack.Page{Top: menuPageSize})
		if err != nil {
			return nil, err
		}
		sprint := pickSprint(agile, sprints, sprintID)
		var board youtrack.SprintBoard
		if sprint != "" {
			if board, err = yt.SprintBoard(ctx, agile.ID, sprint); err != nil {
				return nil, err
			}
		}
		return func() {
			a.boards, a.boardID, a.sprints, a.sprintID, a.board = boards, agile.ID, sprints, sprint, board
		}, nil
	})
}

func pickSprint(agile youtrack.Agile, sprints []youtrack.Sprint, wanted string) string {
	if slices.ContainsFunc(sprints, func(s youtrack.Sprint) bool { return s.ID == wanted }) {
		return wanted
	}
	if agile.CurrentSprint != nil {
		return agile.CurrentSprint.ID
	}
	if len(sprints) == 0 {
		return ""
	}
	return sprints[len(sprints)-1].ID
}

func (a *app) search(skip int) {
	yt, query := a.yt, sortedQuery(a.query, a.sort)
	a.bg("A busca falhou", func(ctx context.Context) (func(), error) {
		found, err := yt.SearchIssues(ctx, query, youtrack.Page{Skip: skip})
		if err != nil {
			return nil, err
		}
		return func() {
			if skip == 0 {
				a.issues = nil
			}
			a.issues = append(a.issues, found...)
			a.more = len(found) == youtrack.DefaultPageSize
		}, nil
	})
}

// sortOrders are the orders the issue list offers; clause is what the search
// takes after the query.
var sortOrders = []struct{ label, clause string }{
	{"Ordem da busca", ""},
	{"Atualizados por último", "sort by: updated desc"},
	{"Criados por último", "sort by: created desc"},
	{"Criados primeiro", "sort by: created asc"},
	{"Atualizados há mais tempo", "sort by: updated asc"},
}

const sortKeyword = "sort by:"

// sortedQuery is the query in the order chosen. A query that names its own
// order keeps it: YouTrack would take two as a compound sort.
func sortedQuery(query string, order int) string {
	if order <= 0 || order >= len(sortOrders) || strings.Contains(strings.ToLower(query), sortKeyword) {
		return query
	}
	return strings.TrimSpace(query + " " + sortOrders[order].clause)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (a *app) saveSettings() {
	if err := SaveSettings(a.dataDir, Settings{CardFields: a.cardFields, Board: a.boardID}); err != nil {
		a.fail("Não foi possível salvar as preferências", err)
	}
}

func (a *app) loadSaved() {
	if a.yt == nil || a.saved != nil {
		return
	}
	yt := a.yt
	a.bg("Não foi possível carregar as buscas salvas", func(ctx context.Context) (func(), error) {
		saved, err := yt.SavedQueries(ctx, youtrack.Page{Top: menuPageSize})
		return func() { a.saved = saved }, err
	})
}

func (a *app) loadLichProjects() {
	conn := a.conn
	a.bg("Não foi possível listar os projetos do lich", func(ctx context.Context) (func(), error) {
		projects, err := conn.Projects(ctx)
		return func() { a.lichProjects = projects }, err
	})
}

func (a *app) issueLists() [][]youtrack.Issue {
	lists := [][]youtrack.Issue{a.issues, a.board.Unmatched}
	for _, c := range a.board.Columns {
		lists = append(lists, c.Issues)
	}
	return lists
}

// boardFields names every custom field the cards on screen carry, on the
// board or in the issue list, in the
// order the first card to have each lists them.
func (a *app) boardFields() []string {
	lists := [][]youtrack.Issue{a.issues}
	if a.tab != tabIssues {
		lists = nil
		for _, c := range a.board.Columns {
			lists = append(lists, c.Issues)
		}
	}
	var names []string
	for _, issue := range slices.Concat(lists...) {
		for _, f := range issue.CustomFields {
			if !slices.Contains(names, f.Name) {
				names = append(names, f.Name)
			}
		}
	}
	return names
}

func (a *app) toggleCardField(name string) {
	if at := slices.Index(a.cardFields, name); at >= 0 {
		a.cardFields = slices.Delete(slices.Clone(a.cardFields), at, at+1)
	} else {
		a.cardFields = append(slices.Clone(a.cardFields), name)
	}
	a.saveSettings()
}

// loadAvatars fetches the pictures on screen that were not asked for yet. A
// picture that fails is only logged: the initial stands in for it.
func (a *app) loadAvatars() {
	if a.yt == nil {
		return
	}
	if a.avatars == nil {
		a.avatars = map[string][]byte{}
	}
	var missing []string
	want := func(avatarURL string) {
		if _, asked := a.avatars[avatarURL]; avatarURL != "" && !asked {
			a.avatars[avatarURL] = nil
			missing = append(missing, avatarURL)
		}
	}
	for _, list := range a.issueLists() {
		for _, issue := range list {
			want(assignee(issue).AvatarURL)
		}
	}
	if a.sel != nil {
		for _, c := range a.sel.comments {
			if c.Author != nil {
				want(c.Author.AvatarURL)
			}
		}
	}
	if len(missing) == 0 {
		return
	}
	yt := a.yt
	a.bg("Não foi possível carregar as fotos", func(ctx context.Context) (func(), error) {
		fetched := map[string][]byte{}
		for _, avatarURL := range missing {
			data, err := yt.Avatar(ctx, avatarURL)
			if err != nil {
				log.Printf("avatar %s: %v", avatarURL, err)
				continue
			}
			fetched[avatarURL] = data
		}
		return func() { maps.Copy(a.avatars, fetched) }, nil
	})
}

func (a *app) findIssue(id string) (youtrack.Issue, bool) {
	for _, list := range a.issueLists() {
		for _, issue := range list {
			if issue.IDReadable == id {
				return issue, true
			}
		}
	}
	return youtrack.Issue{}, false
}

// current is the open card when it still is id: a request answers after the
// person may have opened another.
func (a *app) current(id string) *detail {
	if a.sel == nil || a.sel.issue.IDReadable != id {
		return nil
	}
	return a.sel
}

func (a *app) open(id string) {
	d := &detail{workDate: today, workType: noOption}
	d.issue.IDReadable = id
	if issue, ok := a.findIssue(id); ok {
		d.issue = issue
	}
	a.sel = d
	a.loadDetail(id)
}

func (a *app) loadDetail(id string) {
	yt := a.yt
	a.bg("Não foi possível abrir o card", func(ctx context.Context) (func(), error) {
		issue, err := yt.Issue(ctx, id)
		if err != nil {
			return nil, err
		}
		schema, err := yt.ProjectCustomFields(ctx, issue.Project.ID, youtrack.Page{Top: menuPageSize})
		if err != nil {
			return nil, err
		}
		links, err := yt.Links(ctx, id)
		if err != nil {
			return nil, err
		}
		attachments, err := yt.Attachments(ctx, id, youtrack.Page{})
		if err != nil {
			return nil, err
		}
		return func() {
			if d := a.current(id); d != nil {
				d.issue, d.schema, d.links, d.attachments = issue, schema, links, attachments
				a.loadDetailTab(d)
			}
		}, nil
	})
}

func (a *app) loadDetailTab(d *detail) {
	yt, id, project := a.yt, d.issue.IDReadable, d.issue.Project.ID
	switch d.tab {
	case detailComments:
		a.bg("Não foi possível carregar os comentários", func(ctx context.Context) (func(), error) {
			comments, err := yt.Comments(ctx, id, youtrack.Page{Top: menuPageSize})
			return func() {
				if d := a.current(id); d != nil {
					d.comments = comments
				}
			}, err
		})
	case detailWork:
		a.bg("Não foi possível carregar as horas", func(ctx context.Context) (func(), error) {
			work, err := yt.WorkItems(ctx, id, youtrack.Page{Top: menuPageSize})
			if err != nil {
				return nil, err
			}
			types, err := yt.WorkItemTypes(ctx, project, youtrack.Page{Top: menuPageSize})
			return func() {
				if d := a.current(id); d != nil {
					d.work, d.workTypes = work, types
				}
			}, err
		})
	case detailHistory:
		a.bg("Não foi possível carregar o histórico", func(ctx context.Context) (func(), error) {
			activities, err := yt.Activities(ctx, id, historyCategories, youtrack.Page{Top: menuPageSize})
			return func() {
				if d := a.current(id); d != nil {
					slices.Reverse(activities)
					d.activities = activities
				}
			}, err
		})
	}
}

// updateIssue applies a change that answers with the issue as it is after,
// then fetches the list again: the change may have moved the card.
func (a *app) updateIssue(id string, change func(context.Context, *youtrack.Client) (youtrack.Issue, error)) {
	yt := a.yt
	a.bg("Não foi possível alterar "+id, func(ctx context.Context) (func(), error) {
		issue, err := change(ctx, yt)
		if err != nil {
			return nil, err
		}
		return func() {
			if d := a.current(id); d != nil {
				d.issue = issue
			}
			a.reload()
		}, nil
	})
}

func fieldOptions(f youtrack.ProjectCustomField) []option {
	if f.Bundle == nil {
		return nil
	}
	var options []option
	for _, u := range f.Bundle.Users {
		options = append(options, option{value: u.Login, label: firstNonEmpty(u.FullName, u.Login)})
	}
	for _, v := range f.Bundle.Values {
		if !v.Archived {
			options = append(options, option{value: v.Name, label: v.Name})
		}
	}
	return options
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// setField reads "field index:option index" out of arg; noOption clears the field.
func (a *app) setField(d *detail, arg string) {
	field, choice, ok := indexPair(arg)
	if !ok || field >= len(d.schema) {
		return
	}
	f := d.schema[field]
	build, editable := singleValueFields[f.Field.FieldType.ID]
	options := fieldOptions(f)
	if !editable || choice >= len(options) {
		return
	}
	var update youtrack.CustomFieldUpdate
	if choice == noOption {
		fieldType, err := youtrack.IssueFieldType(f.Field.FieldType.ID)
		if err != nil {
			a.fail("Campo não suportado", err)
			return
		}
		update = youtrack.ClearField(f.Field.Name, fieldType)
	} else {
		update = build(f.Field.Name, options[choice].value)
	}
	id := d.issue.IDReadable
	a.updateIssue(id, func(ctx context.Context, yt *youtrack.Client) (youtrack.Issue, error) {
		return yt.SetCustomFields(ctx, id, update)
	})
}

func indexPair(arg string) (first, second int, ok bool) {
	left, right, found := strings.Cut(arg, argSeparator)
	first, errFirst := strconv.Atoi(left)
	second, errSecond := strconv.Atoi(right)
	return first, second, found && errFirst == nil && errSecond == nil && first >= 0 && second >= noOption
}

// saveEdit sends the title and the description the edit changed, and leaves
// the editor open when YouTrack refuses either, so nothing typed is lost.
func (a *app) saveEdit(d *detail) {
	id, summary, description := d.issue.IDReadable, strings.TrimSpace(d.summaryDraft), d.descDraft
	if summary == "" {
		a.fail("Card sem título", errors.New("informe o título"))
		return
	}
	renamed, rewritten := summary != d.issue.Summary, description != d.issue.Description
	if !renamed && !rewritten {
		d.editing = false
		return
	}
	yt := a.yt
	a.bg("Não foi possível alterar "+id, func(ctx context.Context) (func(), error) {
		var issue youtrack.Issue
		var err error
		if renamed {
			if issue, err = yt.SetSummary(ctx, id, summary); err != nil {
				return nil, err
			}
		}
		if rewritten {
			if issue, err = yt.SetDescription(ctx, id, description); err != nil {
				return nil, err
			}
		}
		return func() {
			if d := a.current(id); d != nil {
				d.issue, d.editing = issue, false
			}
			a.reload()
		}, nil
	})
}

func (a *app) sendComment(d *detail) {
	text := strings.TrimSpace(d.commentDraft)
	if text == "" {
		return
	}
	yt, id := a.yt, d.issue.IDReadable
	a.bg("Não foi possível comentar", func(ctx context.Context) (func(), error) {
		comment, err := yt.AddComment(ctx, id, text)
		return func() {
			if d := a.current(id); d != nil {
				d.comments, d.commentDraft = append(d.comments, comment), ""
			}
		}, err
	})
}

func (a *app) sendWork(d *detail) {
	minutes, err := parseDuration(d.workDuration)
	if err != nil {
		a.fail("Apontamento inválido", err)
		return
	}
	day, err := parseDay(d.workDate, time.Now())
	if err != nil {
		a.fail("Apontamento inválido", err)
		return
	}
	in := youtrack.WorkItemInput{Date: day, Minutes: minutes, Text: strings.TrimSpace(d.workText)}
	if d.workType != noOption {
		in.TypeID = d.workTypes[d.workType].ID
	}
	yt, id := a.yt, d.issue.IDReadable
	a.bg("Não foi possível apontar o tempo", func(ctx context.Context) (func(), error) {
		item, err := yt.AddWorkItem(ctx, id, in)
		return func() {
			if d := a.current(id); d != nil {
				d.work, d.workDuration, d.workText = append(d.work, item), "", ""
			}
			a.toast("Tempo apontado em " + id)
		}, err
	})
}

func (a *app) issueURL(id string) string {
	return a.cfg.BaseURL + "/issue/" + id
}

func (a *app) openSession(d *detail, arg string) {
	i, err := strconv.Atoi(arg)
	if err != nil || i >= len(a.lichProjects) {
		return
	}
	id := d.issue.IDReadable
	description := d.issue.Description
	if len(description) > promptDescriptionLimit {
		description = strings.ToValidUTF8(description[:promptDescriptionLimit], "") + "…"
	}
	params := extension.OpenSessionParams{
		Project: a.lichProjects[i].Path,
		Prompt: fmt.Sprintf("Trabalhe no card %s do YouTrack: %s\n%s\n\n%s",
			id, d.issue.Summary, a.issueURL(id), description),
	}
	conn := a.conn
	a.bg("Não foi possível abrir a sessão", func(ctx context.Context) (func(), error) {
		opened, err := conn.OpenSession(ctx, params)
		if err != nil {
			return nil, err
		}
		return func() {
			if opened.TaskError != "" {
				a.fail("Sessão aberta, mas o card não chegou nela", errors.New(opened.TaskError))
				return
			}
			a.toast(fmt.Sprintf("Sessão %s aberta com %s", opened.Label, id))
		}, nil
	})
}

// moveCard moves the card on screen first and asks YouTrack after: lich draws
// nothing until the extension renders. A refusal fetches the board again,
// which puts the card back.
func (a *app) moveCard(e extension.EventData, arg string) {
	to, err := strconv.Atoi(arg)
	from, okFrom := columnIndex(e.From)
	if err != nil || !okFrom || to >= len(a.board.Columns) || from >= len(a.board.Columns) || from == to {
		return
	}
	source := a.board.Columns[from].Issues
	at := slices.IndexFunc(source, func(i youtrack.Issue) bool { return i.IDReadable == e.Key })
	if at < 0 {
		return
	}
	issue := source[at]
	a.board.Columns[from].Issues = slices.Delete(slices.Clone(source), at, at+1)
	target := a.board.Columns[to].Issues
	a.board.Columns[to].Issues = slices.Insert(slices.Clone(target), min(e.Index, len(target)), issue)

	yt, agile, column := a.yt, a.board.Agile, a.board.Columns[to].Column
	a.bg("Não foi possível mover "+e.Key, func(ctx context.Context) (func(), error) {
		moved, err := yt.MoveCardToColumn(ctx, e.Key, agile, column)
		return func() {
			if err != nil {
				a.fail("Não foi possível mover "+e.Key, err)
				a.reload()
				return
			}
			if d := a.current(e.Key); d != nil {
				d.issue = moved
			}
		}, nil
	})
}

const columnPrefix = "column:"

func columnKey(i int) string { return columnPrefix + strconv.Itoa(i) }

func columnIndex(key string) (int, bool) {
	rest, ok := strings.CutPrefix(key, columnPrefix)
	i, err := strconv.Atoi(rest)
	return i, ok && err == nil && i >= 0
}

func (a *app) openCreate() {
	a.create = createForm{open: true, desc: descriptionTemplate}
	a.focus = summaryKey
	yt, boardProjects := a.yt, a.board.Agile.Projects
	a.bg("Não foi possível listar os projetos", func(ctx context.Context) (func(), error) {
		all, err := yt.Projects(ctx, youtrack.Page{Top: menuPageSize})
		if err != nil {
			return nil, err
		}
		projects := slices.DeleteFunc(all, func(p youtrack.Project) bool { return p.Archived })
		return func() {
			if !a.create.open {
				return
			}
			a.create.projects = projects
			// A card created from a board most likely belongs to its project.
			a.create.project = max(0, slices.IndexFunc(projects, func(p youtrack.Project) bool {
				return len(boardProjects) > 0 && p.ID == boardProjects[0].ID
			}))
			a.loadCreateSchema()
		}, nil
	})
}

func (a *app) loadCreateSchema() {
	a.create.schema, a.create.values, a.create.multi = nil, map[string]string{}, map[string][]string{}
	if a.create.project >= len(a.create.projects) {
		return
	}
	yt, project := a.yt, a.create.projects[a.create.project].ID
	a.bg("Não foi possível ler os campos do projeto", func(ctx context.Context) (func(), error) {
		schema, err := yt.ProjectCustomFields(ctx, project, youtrack.Page{Top: menuPageSize})
		return func() {
			c := &a.create
			if c.open && c.project < len(c.projects) && c.projects[c.project].ID == project {
				c.schema = schema
			}
		}, err
	})
}

func (a *app) chooseCreateField(arg string) {
	field, choice, ok := indexPair(arg)
	if !ok || field >= len(a.create.schema) {
		return
	}
	f := a.create.schema[field]
	if choice == noOption {
		delete(a.create.values, f.Field.Name)
		return
	}
	if options := fieldOptions(f); choice < len(options) {
		a.create.values[f.Field.Name] = options[choice].value
	}
}

func (a *app) createIssue() {
	c := a.create
	summary := strings.TrimSpace(c.summary)
	if summary == "" || c.project >= len(c.projects) {
		a.fail("Card incompleto", errors.New("escolha o projeto e informe o título"))
		return
	}
	fields, err := createFields(c)
	if err != nil {
		a.fail("Campo inválido", err)
		return
	}
	in := youtrack.NewIssue{ProjectID: c.projects[c.project].ID, Summary: summary, Description: c.desc, CustomFields: fields}
	yt := a.yt
	a.bg("Não foi possível criar o card", func(ctx context.Context) (func(), error) {
		issue, err := yt.CreateIssue(ctx, in)
		if err != nil {
			return nil, err
		}
		return func() {
			a.create = createForm{}
			a.toast("Card " + issue.IDReadable + " criado")
			a.reload()
			a.open(issue.IDReadable)
		}, nil
	})
}

// saveConfig keeps the connection only after YouTrack accepted the token.
func (a *app) saveConfig() {
	cfg := youtrack.Config{
		BaseURL:  strings.TrimRight(strings.TrimSpace(a.config.url), "/"),
		Token:    strings.TrimSpace(a.config.token),
		Insecure: a.config.insecure,
	}
	if !strings.HasPrefix(cfg.BaseURL, "https://") && !strings.HasPrefix(cfg.BaseURL, "http://") {
		a.fail("URL inválida", errors.New("a URL começa com https:// ou http://"))
		return
	}
	if cfg.Token == "" {
		a.fail("Token vazio", errors.New("informe um token permanente do YouTrack"))
		return
	}
	dir := a.dataDir
	a.bg("Não foi possível conectar ao YouTrack", func(ctx context.Context) (func(), error) {
		me, err := cfg.Client().Me(ctx)
		if err != nil {
			return nil, err
		}
		if err := youtrack.SaveConfig(dir, cfg); err != nil {
			return nil, err
		}
		return func() {
			a.connect(cfg)
			a.config, a.saved, a.sel, a.avatars = configForm{}, nil, nil, nil
			a.toast("Conectado como " + firstNonEmpty(me.FullName, me.Login))
			a.loadSaved()
			a.reload()
		}, nil
	})
}
