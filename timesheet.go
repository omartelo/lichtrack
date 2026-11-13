package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/lichdotdev/lich/extension"

	"github.com/omartelo/lichtrack/youtrack"
)

const (
	daysInWeek     = 7
	sheetDayWidth  = 64
	sheetDayLayout = "02/01"
	sheetCellEmpty = "·"
	// boardRefresh is how often the board on screen is fetched again.
	boardRefresh = time.Minute
)

var weekdays = [daysInWeek]string{"seg", "ter", "qua", "qui", "sex", "sáb", "dom"}

type timesheet struct {
	// week is the Monday, at midnight UTC, of the week on screen.
	week  time.Time
	items []youtrack.WorkItem

	issue    string
	date     string
	duration string
	text     string
}

type sheetRow struct {
	id      string
	summary string
	days    [daysInWeek]int
	total   int
}

// weekOf is the Monday of t's week, at midnight UTC as parseDay puts a day.
func weekOf(t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -(int(day.Weekday())+daysInWeek-1)%daysInWeek)
}

// sheetRows adds up the minutes of each issue on each day of the week that
// starts on monday; what falls outside the week is left out.
func sheetRows(items []youtrack.WorkItem, monday time.Time) ([]sheetRow, [daysInWeek]int) {
	var totals [daysInWeek]int
	byIssue := map[string]*sheetRow{}
	for _, w := range items {
		day := int(time.UnixMilli(w.Date).UTC().Sub(monday).Hours() / 24)
		if w.Issue == nil || w.Date < monday.UnixMilli() || day >= daysInWeek {
			continue
		}
		row := byIssue[w.Issue.IDReadable]
		if row == nil {
			row = &sheetRow{id: w.Issue.IDReadable, summary: w.Issue.Summary}
			byIssue[row.id] = row
		}
		row.days[day] += w.Duration.Minutes
		row.total += w.Duration.Minutes
		totals[day] += w.Duration.Minutes
	}
	rows := make([]sheetRow, 0, len(byIssue))
	for _, row := range byIssue {
		rows = append(rows, *row)
	}
	slices.SortFunc(rows, func(a, b sheetRow) int { return strings.Compare(a.id, b.id) })
	return rows, totals
}

func (a *app) loadTimesheet() {
	if a.sheet.week.IsZero() {
		a.sheet.week, a.sheet.date = weekOf(time.Now()), today
	}
	yt, monday := a.yt, a.sheet.week
	a.bg("Não foi possível carregar o timesheet", func(ctx context.Context) (func(), error) {
		items, err := yt.WorkItemsBetween(ctx, youtrack.AuthorMe, monday, monday.AddDate(0, 0, daysInWeek-1), youtrack.Page{Top: menuPageSize})
		return func() {
			if a.sheet.week.Equal(monday) {
				a.sheet.items = items
			}
		}, err
	})
}

func (a *app) sheetEvent(name, arg string, e extension.EventData) bool {
	switch name {
	case hSheetWeek:
		weeks := map[string]int{sheetPrev: -1, sheetNext: 1}[arg]
		a.sheet.week = a.sheet.week.AddDate(0, 0, weeks*daysInWeek)
		if arg == sheetNow {
			a.sheet.week = weekOf(time.Now())
		}
		a.sheet.items = nil
		a.loadTimesheet()
	case hSheetCell:
		// arg is "issue:day", a cell of the grid: logging more there is one press away.
		a.sheet.issue, a.sheet.date, _ = strings.Cut(arg, argSeparator)
		a.focus = sheetDurationKey
	case hSheetIssue:
		a.sheet.issue = e.Text
	case hSheetDate:
		a.sheet.date = e.Text
	case hSheetDuration:
		a.sheet.duration = e.Text
	case hSheetText:
		a.sheet.text = e.Text
	case hSheetSend:
		a.logTime()
	default:
		return false
	}
	return true
}

func (a *app) logTime() {
	s := a.sheet
	id := strings.ToUpper(strings.TrimSpace(s.issue))
	if id == "" {
		a.fail("Lançamento inválido", errors.New("informe o card, por exemplo LOJ-123"))
		return
	}
	minutes, err := parseDuration(s.duration)
	if err != nil {
		a.fail("Lançamento inválido", err)
		return
	}
	day, err := parseDay(s.date, time.Now())
	if err != nil {
		a.fail("Lançamento inválido", err)
		return
	}
	in := youtrack.WorkItemInput{Date: day, Minutes: minutes, Text: strings.TrimSpace(s.text)}
	yt := a.yt
	a.bg("Não foi possível lançar o tempo em "+id, func(ctx context.Context) (func(), error) {
		if _, err := yt.AddWorkItem(ctx, id, in); err != nil {
			return nil, err
		}
		return func() {
			a.sheet.duration, a.sheet.text = "", ""
			a.toast("Lançado " + formatMinutes(minutes) + " em " + id)
			a.loadTimesheet()
		}, nil
	})
}

func sheetCell(label string, weight extension.FontWeight) node {
	return extension.Box.New(box{Width: sheetDayWidth, Align: extension.AlignEnd},
		extension.Label.New(extension.LabelProps{Text: label, Weight: weight, Color: mutedColor}))
}

func (a *app) timesheetView() node {
	monday := a.sheet.week
	rows, totals := sheetRows(a.sheet.items, monday)
	week := 0
	head := []node{grow()}
	foot := []node{grow(extension.Label.New(extension.LabelProps{Text: "Total", Weight: extension.FontSemibold}))}
	for i, name := range weekdays {
		week += totals[i]
		head = append(head, sheetCell(name+" "+monday.AddDate(0, 0, i).Format(sheetDayLayout), extension.FontRegular))
		foot = append(foot, sheetCell(minutesOrEmpty(totals[i]), extension.FontSemibold))
	}
	head = append(head, sheetCell("total", extension.FontRegular))
	foot = append(foot, sheetCell(formatMinutes(week), extension.FontSemibold))

	grid := []node{row(head...), extension.Separator.New(extension.NoProps{})}
	for _, r := range rows {
		line := []node{grow(extension.HoverLink.New(extension.LinkProps{Text: r.id + "  " + r.summary, OnPress: on(hOpen, r.id)}))}
		for i, minutes := range r.days {
			day := monday.AddDate(0, 0, i).Format(isoDay)
			line = append(line, extension.Box.New(box{Width: sheetDayWidth, Align: extension.AlignEnd},
				extension.GhostTextButton.New(extension.GhostTextButtonProps{Label: minutesOrEmpty(minutes), OnPress: on(hSheetCell, r.id, day)})))
		}
		grid = append(grid, row(append(line, sheetCell(formatMinutes(r.total), extension.FontSemibold))...))
	}
	if len(rows) == 0 {
		grid = append(grid, muted("Nenhum tempo lançado nesta semana."))
	}
	grid = append(grid, extension.Separator.New(extension.NoProps{}), row(foot...))

	sunday := monday.AddDate(0, 0, daysInWeek-1)
	nav := row(
		extension.GhostIconButton.New(extension.GhostIconButtonProps{Icon: "chevron-left", Hint: "Semana anterior", OnPress: on(hSheetWeek, sheetPrev)}),
		extension.GhostIconButton.New(extension.GhostIconButtonProps{Icon: "chevron-right", Hint: "Próxima semana", OnPress: on(hSheetWeek, sheetNext)}),
		button("Esta semana", extension.VariantOutline, on(hSheetWeek, sheetNow)),
		grow(extension.Label.New(extension.LabelProps{
			Text: monday.Format(brDay) + " – " + sunday.Format(brDay), Weight: extension.FontSemibold,
		})),
	)
	form := row(
		extension.Input.New(extension.InputProps{Value: a.sheet.issue, Placeholder: "Card (LOJ-123)", Mono: true, Width: workDateWidth + sheetDayWidth, OnChange: hSheetIssue}),
		extension.Input.New(extension.InputProps{Value: a.sheet.date, Placeholder: today, Width: workDateWidth, OnChange: hSheetDate}),
		extension.Input.New(extension.InputProps{Value: a.sheet.duration, Placeholder: "1h 30m", Width: workTimeWidth, OnChange: hSheetDuration, OnSubmit: hSheetSend}).WithKey(sheetDurationKey),
		grow(extension.Input.New(extension.InputProps{Value: a.sheet.text, Placeholder: "Nota", OnChange: hSheetText, OnSubmit: hSheetSend})),
		button("Lançar", extension.VariantDefault, hSheetSend),
	)
	return island(
		extension.Box.New(box{Padding: pad}, nav),
		extension.Separator.New(extension.NoProps{}),
		scrolled(grid...),
		extension.Separator.New(extension.NoProps{}),
		extension.Box.New(box{Padding: pad}, form),
	)
}

func minutesOrEmpty(minutes int) string {
	if minutes == 0 {
		return sheetCellEmpty
	}
	return formatMinutes(minutes)
}

// tick fetches the board again when it is what the person is looking at and
// nothing else is in flight: a reload under a form would only get in the way.
func (a *app) tick() {
	if a.visible && a.yt != nil && a.tab == tabBoard && a.sel == nil && !a.create.open && a.busy == 0 {
		a.loadBoard(a.boardID, a.sprintID)
		a.render()
	}
}
