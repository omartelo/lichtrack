package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lichdotdev/lich/extension"

	"github.com/omartelo/lichtrack/youtrack"
)

const (
	pad = 12
	gap = 8
	// gutter and islandRadius match lich's own islands (native/DESIGN.md): the
	// screen draws none, so an extension draws its own.
	gutter       = 6
	islandRadius = 8

	cardHeight = 48
	// A menu up to this long fits a small window whole.
	menuMaxEntries  = 10
	pickerRows      = 8
	pickerRowHeight = 32
	// pickerWidth is the least a panel is wide; a wider field opens one as wide as itself.
	pickerWidth      = 220
	cardInset        = 6
	cardGap          = 4
	cardMetaSize     = 11
	cardAvatarSize   = 18
	cardSummaryLines = 2
	selectWidth      = 180
	fieldsPanelWidth = 260
	fieldsPadding    = 16
	// fieldControlWidth is the room a control has in the fields' column.
	fieldControlWidth = fieldsPanelWidth - 2*fieldsPadding
	fieldsGap         = 18
	fieldLabelGap     = 6
	fieldLabelSize    = 12
	dialogWidth       = 560
	composerHeight    = 140
	workDateWidth     = 110
	workTimeWidth     = 90
	summarySize       = 16
	boardGroup        = "board"
	mutedColor        = "muted-foreground"
	emptyFieldValue   = "—"

	// lich refuses a tree over 5000 nodes (docs/extensions.md, What lich
	// enforces); a card is a handful of them, so a long column is cut.
	maxCardsPerColumn = 40
	favoriteMark      = "★ "
)

type (
	node = extension.Node
	box  = extension.BoxProps
)

func text(s string) node { return extension.Label.New(extension.LabelProps{Text: s}) }

func muted(s string) node {
	return extension.Label.New(extension.LabelProps{Text: s, Color: mutedColor})
}

func row(children ...node) node {
	return extension.Box.New(box{Direction: extension.DirectionRow, Gap: gap, Align: extension.AlignCenter}, children...)
}

func grow(children ...node) node { return extension.Box.New(box{Grow: 1}, children...) }

func island(children ...node) node {
	return extension.Box.New(box{Grow: 1, Background: "background", Radius: islandRadius}, children...)
}

func button(label string, variant extension.Variant, handler extension.Handler) node {
	return extension.Button.New(extension.ButtonProps{Text: label, Variant: variant, Size: extension.SizeSM, OnPress: handler})
}

// choice is a select. A short list opens as a menu; a long one opens as a
// panel under the field with a filter and its own scroll, since lich draws a
// menu whole and one taller than the window covers the field and runs off it.
// id names the select among those on screen.
func (a *app) choice(id, value string, width float64, entries []extension.MenuEntry) node {
	if len(entries) <= menuMaxEntries {
		return extension.DropdownMenu.New(extension.DropdownMenuProps{Entries: entries},
			extension.SelectTrigger.New(extension.SelectTriggerProps{Value: value, Width: width}))
	}
	shown := filterEntries(entries, a.pickFilters[id])
	rows := make([]node, len(shown))
	for i, e := range shown {
		rows[i] = extension.NavRow.New(
			extension.NavRowProps{On: e.Checked, Height: pickerRowHeight, OnPress: on(hPick, e.OnSelect)},
			// A row lays its content from the top; the box centers the label.
			extension.Box.New(box{Height: pickerRowHeight, Justify: extension.JustifyCenter},
				extension.Label.New(extension.LabelProps{Text: e.Label, MaxLines: 1})),
		)
	}
	if len(rows) == 0 {
		rows = []node{extension.Box.New(box{Padding: gap}, muted("Nada encontrado"))}
	}
	trigger := extension.SelectTrigger.New(extension.SelectTriggerProps{Value: value, Width: width})
	// lich shuts a panel only on a press outside it; a new key is a new panel,
	// which starts shut, so choosing a row bumps pickEpoch.
	return extension.DropdownPanel.New(extension.DropdownPanelProps{Trigger: trigger, Width: max(width, pickerWidth)},
		extension.Box.New(box{Gap: gap},
			extension.Input.New(extension.InputProps{
				Value: a.pickFilters[id], Placeholder: "Filtrar", Leading: "search", Clearable: true, OnChange: on(hPickFilter, id),
			}).WithKey(pickerKey(id)),
			extension.Box.New(box{Height: float64(min(len(rows), pickerRows) * pickerRowHeight)},
				extension.Scroll.New(extension.ScrollProps{}, extension.Box.New(box{}, rows...))),
		),
	).WithKey(fmt.Sprintf("pick:%s:%d", id, a.pickEpoch))
}

func pickerKey(id string) string { return "pick.filter:" + id }

func filterEntries(entries []extension.MenuEntry, filter string) []extension.MenuEntry {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return entries
	}
	var found []extension.MenuEntry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Label), filter) {
			found = append(found, e)
		}
	}
	return found
}

func entry(label string, checked bool, handler extension.Handler) extension.MenuEntry {
	return extension.MenuEntry{Kind: extension.MenuRadioItem, Label: label, Checked: checked, OnSelect: handler}
}

func (a *app) render() {
	a.send(extension.Render.New(extension.RenderData{Surface: surfaceID, Root: a.root(), Shortcuts: a.shortcuts()}))
}

func (a *app) shortcuts() []extension.Shortcut {
	if a.yt == nil {
		return nil
	}
	keys := []extension.Shortcut{
		{Keys: "/", Handler: hFind}, {Keys: "c", Handler: hNew}, {Keys: "r", Handler: hReload},
		{Keys: "1", Handler: on(hTab, tabBoard)}, {Keys: "2", Handler: on(hTab, tabIssues)}, {Keys: "3", Handler: on(hTab, tabSheet)},
	}
	if a.create.open {
		return append(keys, extension.Shortcut{Keys: "escape", Handler: hNewCancel},
			extension.Shortcut{Keys: "ctrl+enter", Handler: hNewCreate})
	}
	if a.sel == nil {
		return keys
	}
	keys = append(keys, extension.Shortcut{Keys: "escape", Handler: hClose})
	if a.sel.editing {
		keys = append(keys, extension.Shortcut{Keys: "ctrl+s", Handler: hEditSave})
	}
	if a.sel.tab == detailComments {
		keys = append(keys, extension.Shortcut{Keys: "ctrl+enter", Handler: hCommentSend})
	}
	return keys
}

func (a *app) root() node {
	if a.yt == nil {
		return island(
			extension.EmptyState.New(extension.EmptyStateProps{
				Icon: "kanban", Title: "Conecte o YouTrack",
				Description: "Informe a URL da instância e um token permanente.",
			}, button("Configurar", extension.VariantDefault, hConfig)),
			a.configDialog(),
		)
	}
	body := a.list()
	switch {
	case a.create.open:
		body = a.createView()
	case a.sel != nil:
		body = a.cardView(a.sel)
	}
	return extension.Box.New(box{Grow: 1, Gap: gutter}, a.toolbar(), body, a.configDialog())
}

func (a *app) toolbar() node {
	items := []node{extension.Tabs.New(extension.TabsProps{
		Selected: a.tab, OnChange: hTab,
		Triggers: []extension.TabsTrigger{{Text: "Board", Icon: "kanban"}, {Text: "Issues", Icon: "list"}, {Text: "Timesheet", Icon: "clock"}},
	})}
	if a.tab == tabBoard && len(a.boards) > 0 {
		items = append(items, a.boardSelect(), a.sprintSelect())
	}
	if a.tab != tabSheet {
		items = append(items, a.cardFieldsMenu())
	}
	items = append(items, grow(extension.Input.New(extension.InputProps{
		Value: a.query, Placeholder: "Busca do YouTrack", Leading: "search", Mono: true, Clearable: true,
		OnChange: hQuery, OnSubmit: hSearch,
	}).WithKey(searchKey)))
	if a.busy > 0 {
		items = append(items, extension.Spinner.New(extension.SpinnerProps{}))
	}
	items = append(items,
		extension.Button.New(extension.ButtonProps{Text: "Novo", Icon: "plus", Size: extension.SizeSM, OnPress: hNew}),
		extension.GhostIconButton.New(extension.GhostIconButtonProps{Icon: "refresh-cw", Hint: "Recarregar", OnPress: hReload}),
		extension.GhostIconButton.New(extension.GhostIconButtonProps{Icon: "settings", Hint: "Conexão", OnPress: hConfig}),
	)
	return extension.Box.New(box{
		Direction: extension.DirectionRow, Gap: gap, Padding: gap, Align: extension.AlignCenter,
		Background: "background", Radius: islandRadius,
	}, items...)
}

func (a *app) boardSelect() node {
	entries := make([]extension.MenuEntry, len(a.boards))
	for i, b := range a.boards {
		name := b.Name
		if b.Favorite {
			name = favoriteMark + name
		}
		entries[i] = entry(name, b.ID == a.boardID, on(hBoard, b.ID))
	}
	return a.choice(hBoard, a.board.Agile.Name, selectWidth, entries)
}

func (a *app) sprintSelect() node {
	var entries []extension.MenuEntry
	for _, s := range a.sprints {
		if !s.Archived || s.ID == a.sprintID {
			entries = append(entries, entry(s.Name, s.ID == a.sprintID, on(hSprint, s.ID)))
		}
	}
	return a.choice(hSprint, firstNonEmpty(a.board.Sprint.Name, "Sem sprint"), selectWidth, entries)
}

func (a *app) cardFieldsMenu() node {
	names := a.boardFields()
	entries := make([]extension.MenuEntry, len(names), len(names)+1)
	for i, name := range names {
		entries[i] = extension.MenuEntry{
			Kind: extension.MenuCheckboxItem, Label: name,
			Checked: slices.Contains(a.cardFields, name), OnCheckedChange: on(hCardField, name),
		}
	}
	if len(entries) == 0 {
		entries = append(entries, extension.MenuEntry{Kind: extension.MenuLabel, Label: "Nenhum campo nos cards"})
	}
	return extension.DropdownMenu.New(extension.DropdownMenuProps{Entries: entries}, extension.Button.New(extension.ButtonProps{
		Text: "Campos", Icon: "sliders-horizontal", Variant: extension.VariantGhost, Size: extension.SizeSM,
	}))
}

func (a *app) list() node {
	switch a.tab {
	case tabIssues:
		return a.issuesView()
	case tabSheet:
		return a.timesheetView()
	}
	return a.boardView()
}

func (a *app) boardView() node {
	if len(a.board.Columns) == 0 {
		return island(extension.EmptyState.New(extension.EmptyStateProps{
			Icon: "kanban", Title: "Nenhum board para mostrar",
			Description: "O YouTrack não devolveu board ou sprint para este usuário.",
		}))
	}
	columns := make([]node, len(a.board.Columns))
	for i, c := range a.board.Columns {
		columns[i] = a.column(i, c)
	}
	return extension.Box.New(box{
		Direction: extension.DirectionRow, Grow: 1, Gap: pad, Padding: pad,
		Background: "background", Radius: islandRadius,
	}, columns...)
}

func (a *app) column(i int, c youtrack.BoardColumn) node {
	shown := c.Issues[:min(len(c.Issues), maxCardsPerColumn)]
	cards := make([]node, len(shown))
	for j, issue := range shown {
		cards[j] = a.card(issue)
	}
	// Every list of the group carries a key and takes OnMove: an empty column
	// still takes a drop.
	list := extension.SortableList.New(extension.SortableListProps{
		Group: boardGroup, OnMove: on(hMove, i),
	}, cards...).WithKey(columnKey(i))
	header := row(
		extension.FormLabel.New(extension.FormLabelProps{Text: c.Column.Presentation, Uppercase: true}),
		muted(strconv.Itoa(len(c.Issues))),
	)
	// The list is the scroll's own child: lich ends the room it lights for a
	// drop with the scroll's viewport only from there (manifest minLichVersion).
	body := []node{header, extension.Scroll.New(extension.ScrollProps{}, list)}
	if hidden := len(c.Issues) - len(shown); hidden > 0 {
		body = append(body, muted(fmt.Sprintf("+%d não exibidos", hidden)))
	}
	return extension.Box.New(box{Grow: 1, Gap: gap}, body...)
}

func (a *app) card(issue youtrack.Issue) node {
	id := issue.IDReadable
	// The inset is the card's, not the list's gap: lich lights a drop target
	// as wide as its list, and a card that wide would cover the light's edge.
	return extension.Box.New(box{PaddingX: cardInset, PaddingY: cardInset / 2},
		extension.PopoverRow.New(extension.PopoverRowProps{OnPress: on(hOpen, id)}, a.cardFace(issue))).WithKey(id)
}

func (a *app) cardFace(issue youtrack.Issue) node {
	top := []node{grow(extension.Label.New(extension.LabelProps{
		Text: issue.IDReadable, Mono: true, Size: cardMetaSize, Color: mutedColor,
	}))}
	if who := assignee(issue); who.Login != "" {
		top = append(top, avatar(a.avatars[who.AvatarURL], userName(who), cardAvatarSize))
	}
	face := []node{row(top...), extension.Label.New(extension.LabelProps{Text: issue.Summary, MaxLines: cardSummaryLines})}
	var tags []node
	for _, name := range a.cardFields {
		if value := fieldValue(issue, name); value != "" {
			tags = append(tags, extension.Tooltip.New(extension.TooltipProps{Text: name},
				extension.Tag.New(extension.TagProps{Text: value})))
		}
	}
	if len(tags) > 0 {
		face = append(face, extension.Wrap.New(extension.WrapProps{GapX: cardGap, GapY: cardGap}, tags...))
	}
	return extension.Box.New(box{Gap: cardGap}, face...)
}

func initial(name string) string {
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return ""
}

func (a *app) issueRow(issue youtrack.Issue) node {
	return a.issueFace(issue, on(hOpen, issue.IDReadable)).WithKey(issue.IDReadable)
}

func (a *app) issueFace(issue youtrack.Issue, onPress extension.Handler) node {
	under := issue.IDReadable
	if who := assignee(issue); who.Login != "" {
		under += " · " + userName(who)
	}
	for _, name := range a.cardFields {
		if value := fieldValue(issue, name); value != "" {
			under += " · " + value
		}
	}
	return extension.NavRow.New(
		extension.NavRowProps{Height: cardHeight, OnPress: onPress},
		extension.TwoLines.New(extension.TwoLinesProps{Top: issue.Summary, Under: under}),
	)
}

// assignee is the first user a card's fields name, zero when none does: the
// field's own name changes from project to project.
func assignee(issue youtrack.Issue) youtrack.FieldValue {
	for _, f := range issue.CustomFields {
		values, _ := f.Values()
		for _, v := range values {
			if v.Login != "" {
				return v
			}
		}
	}
	return youtrack.FieldValue{}
}

func userName(v youtrack.FieldValue) string { return firstNonEmpty(v.FullName, v.Login) }

func avatar(image []byte, name string, size float64) node {
	return extension.Tooltip.New(extension.TooltipProps{Text: name},
		extension.Avatar.New(extension.AvatarProps{Image: image, Initial: initial(name), Size: size}))
}

func (a *app) issuesView() node {
	orders := make([]extension.MenuEntry, len(sortOrders))
	for i, o := range sortOrders {
		orders[i] = entry(o.label, i == a.sort, on(hSort, i))
	}
	bar := []node{a.choice(hSort, sortOrders[a.sort].label, selectWidth, orders)}
	if len(a.saved) > 0 {
		all := make([]extension.MenuEntry, len(a.saved))
		for i, q := range a.saved {
			all[i] = entry(q.Name, q.Query == a.query, on(hSaved, i))
		}
		bar = append(bar, a.choice(hSaved, "Buscas salvas", selectWidth, all))
	}
	// The searches pinned in YouTrack's sidebar are one press away; the rest
	// are in the select.
	for i, q := range a.saved {
		if q.Pinned {
			// A button, not a chip: lich draws a chip's count, and a 0 there
			// reads as a search that found nothing.
			variant := extension.VariantGhost
			if q.Query == a.query {
				variant = extension.VariantSecondary
			}
			bar = append(bar, button(q.Name, variant, on(hSaved, i)))
		}
	}
	children := []node{extension.Box.New(box{Padding: pad},
		extension.Wrap.New(extension.WrapProps{GapX: gap, GapY: gap}, bar...))}
	if len(a.issues) == 0 {
		return island(append(children, extension.EmptyState.New(extension.EmptyStateProps{
			Icon: "search", Title: "Nenhum card encontrado", Description: a.query,
		}))...)
	}
	rows := make([]node, len(a.issues), len(a.issues)+1)
	for i, issue := range a.issues {
		rows[i] = a.issueRow(issue)
	}
	if a.more {
		rows = append(rows, extension.Box.New(box{Padding: gap, Align: extension.AlignCenter},
			button("Carregar mais", extension.VariantGhost, hMore)))
	}
	return island(append(children, extension.Scroll.New(extension.ScrollProps{},
		extension.Box.New(box{PaddingX: gap}, rows...)))...)
}

// cardView is the card alone on the screen, as YouTrack shows one: what it
// says on the left, its fields down the right.
func (a *app) cardView(d *detail) node {
	id := d.issue.IDReadable
	header := row(
		extension.GhostIconButton.New(extension.GhostIconButtonProps{Icon: "arrow-left", Hint: "Voltar", OnPress: hClose}),
		extension.Label.New(extension.LabelProps{Text: id, Mono: true, Color: mutedColor}),
		grow(muted(cardHistory(d.issue))),
		a.sessionButton(),
		extension.GhostIconButton.New(extension.GhostIconButtonProps{Icon: "external-link", Hint: "Abrir no YouTrack", OnPress: on(hExternal, id)}),
	)
	comments := "Comentários"
	if len(d.comments) > 0 {
		comments = fmt.Sprintf("Comentários (%d)", len(d.comments))
	}
	heading := []node{header, summaryNode(d)}
	if len(d.issue.Tags) > 0 {
		tags := make([]node, len(d.issue.Tags))
		for i, t := range d.issue.Tags {
			tags[i] = extension.Tag.New(extension.TagProps{Text: t.Name})
		}
		heading = append(heading, extension.Wrap.New(extension.WrapProps{GapX: gap, GapY: gap}, tags...))
	}
	top := extension.Box.New(box{Padding: pad, Gap: gap}, append(heading,
		extension.Tabs.New(extension.TabsProps{
			Selected: d.tab, Variant: extension.TabsVariantLine, OnChange: hDetailTab,
			Triggers: []extension.TabsTrigger{{Text: "Descrição"}, {Text: comments}, {Text: "Horas"}, {Text: "Histórico"}},
		}))...)
	content := island(append([]node{top, extension.Separator.New(extension.NoProps{})}, a.detailBody(d)...)...)
	fields := append([]node{field("Projeto", text(d.issue.Project.Name))}, a.fieldNodes(d)...)
	fields = append(fields, relatedNodes(d)...)
	side := extension.Box.New(box{Width: fieldsPanelWidth, Background: "sidebar", Radius: islandRadius},
		extension.Scroll.New(extension.ScrollProps{}, extension.Box.New(box{Padding: fieldsPadding, Gap: fieldsGap}, fields...)))
	return extension.Box.New(box{Direction: extension.DirectionRow, Grow: 1, Gap: gutter}, content, side)
}

// relatedNodes is what hangs off the card, for the foot of the fields'
// column: each kind of link with its cards, and the attachments.
func relatedNodes(d *detail) []node {
	var related []node
	for _, l := range d.links {
		if len(l.Issues) == 0 {
			continue
		}
		relation := l.LinkType.SourceToTarget
		if l.Direction == "INWARD" {
			relation = l.LinkType.TargetToSource
		}
		cards := make([]node, len(l.Issues))
		for i, linked := range l.Issues {
			cards[i] = extension.Tooltip.New(extension.TooltipProps{Text: linked.Summary},
				extension.HoverLink.New(extension.LinkProps{
					Text: linked.IDReadable + "  " + linked.Summary, OnPress: on(hOpen, linked.IDReadable),
				}))
		}
		related = append(related, field(firstNonEmpty(relation, l.LinkType.Name), extension.Box.New(box{Gap: cardGap}, cards...)))
	}
	if len(d.attachments) > 0 {
		files := make([]node, len(d.attachments))
		for i, at := range d.attachments {
			files[i] = extension.Tooltip.New(extension.TooltipProps{Text: at.Name},
				extension.HoverLink.New(extension.LinkProps{Text: at.Name, OnPress: on(hAttachment, i)}))
		}
		related = append(related, field("Anexos", extension.Box.New(box{Gap: cardGap}, files...)))
	}
	return related
}

// cardHistory is who created the card and when, and when it last changed.
func cardHistory(issue youtrack.Issue) string {
	if issue.Created == 0 {
		return ""
	}
	line := "Criado por " + firstNonEmpty(authorName(issue.Reporter), "?") + " em " + formatMillis(issue.Created)
	if issue.Updated > issue.Created {
		line += " · Atualizado em " + formatMillis(issue.Updated)
	}
	return line
}

func field(name string, control node) node {
	return extension.Box.New(box{Gap: fieldLabelGap},
		extension.Label.New(extension.LabelProps{Text: name, Size: fieldLabelSize, Color: mutedColor}), control)
}

// summaryNode is the title, a field while the card is being edited.
func summaryNode(d *detail) node {
	if d.editing {
		return extension.Input.New(extension.InputProps{
			Value: d.summaryDraft, TextSize: summarySize, OnChange: hSummaryDraft, OnSubmit: hEditSave,
		}).WithKey("summary:" + d.issue.IDReadable)
	}
	return row(
		grow(extension.Label.New(extension.LabelProps{Text: d.issue.Summary, Size: summarySize, Weight: extension.FontSemibold})),
		extension.GhostIconButton.New(extension.GhostIconButtonProps{Icon: "pencil", Hint: "Editar título e descrição", OnPress: hEdit}),
	)
}

func (a *app) sessionButton() node {
	props := extension.ButtonProps{Text: "Abrir sessão", Icon: "play", Variant: extension.VariantOutline, Size: extension.SizeSM}
	switch len(a.lichProjects) {
	case 0:
		props.Disabled = true
		return extension.Button.New(props)
	case 1:
		props.OnPress = on(hSession, 0)
		return extension.Button.New(props)
	}
	entries := make([]extension.MenuEntry, len(a.lichProjects))
	for i, p := range a.lichProjects {
		entries[i] = extension.MenuEntry{Kind: extension.MenuItem, Label: p.Name, OnSelect: on(hSession, i)}
	}
	return extension.DropdownMenu.New(extension.DropdownMenuProps{Entries: entries}, extension.Button.New(props))
}

func fieldValue(issue youtrack.Issue, name string) string {
	for _, f := range issue.CustomFields {
		if f.Name != name {
			continue
		}
		values, _ := f.Values()
		if len(values) == 0 {
			// A string or a number: the field holds no entity.
			return activityValue(f.Value)
		}
		names := make([]string, len(values))
		for i, v := range values {
			names[i] = firstNonEmpty(v.FullName, v.Name, v.Presentation, v.Login, v.Text)
		}
		return strings.Join(names, ", ")
	}
	return ""
}

func (a *app) fieldNodes(d *detail) []node {
	var fields []node
	for i, f := range d.schema {
		name, kind := f.Field.Name, f.Field.FieldType.ID
		value := fieldValue(d.issue, name)
		_, single := singleValueFields[kind]
		_, multi := multiValueFields[kind]
		var control node
		switch {
		case single:
			control = a.choice(string(on(hField, i)), firstNonEmpty(value, emptyFieldValue), fieldControlWidth, fieldEntries(f, value, hField, i))
		case multi:
			control = a.multiChoice(f, valueKeys(d.issue, name), emptyFieldValue, hFieldToggle, i)
		case kind == periodFieldType:
			// Keyed by the card: what was typed into one never lands on another.
			control = extension.Input.New(extension.InputProps{
				Value: value, Placeholder: periodPlaceholder, OnSubmit: on(hFieldText, i),
			}).WithKey(fmt.Sprintf("period:%s:%d", d.issue.IDReadable, i))
		case value != "":
			control = text(value)
		default:
			continue
		}
		fields = append(fields, field(name, control))
	}
	return fields
}

func fieldEntries(f youtrack.ProjectCustomField, current string, handler string, field int) []extension.MenuEntry {
	var entries []extension.MenuEntry
	if f.CanBeEmpty {
		entries = append(entries, entry(firstNonEmpty(f.EmptyFieldText, emptyFieldValue), current == "", on(handler, field, noOption)))
	}
	for i, o := range fieldOptions(f) {
		entries = append(entries, entry(o.label, o.label == current, on(handler, field, i)))
	}
	return entries
}

func (a *app) detailBody(d *detail) []node {
	switch d.tab {
	case detailComments:
		return commentsBody(d, a.avatars)
	case detailWork:
		return a.workBody(d)
	case detailHistory:
		return historyBody(d)
	}
	return descriptionBody(d)
}

func scrolled(children ...node) node {
	return extension.Scroll.New(extension.ScrollProps{}, extension.Box.New(box{Padding: pad, Gap: gap}, children...))
}

func descriptionBody(d *detail) []node {
	id := d.issue.IDReadable
	if d.editing {
		toggle := "Prévia"
		editor := grow(extension.Textarea.New(extension.TextareaProps{
			Value: d.descDraft, Grow: true, OnChange: hDescDraft,
		}).WithKey("desc:" + id))
		if d.preview {
			toggle = "Editar"
			editor = scrolled(extension.Markdown.New(extension.MarkdownProps{Text: d.descDraft, OnOpen: hLink}))
		}
		return []node{
			extension.Box.New(box{Padding: pad, Grow: 1, Gap: gap},
				row(button(toggle, extension.VariantOutline, hDescPreview), grow(),
					extension.KeyHint.New(extension.KeyHintProps{Keys: "ctrl+s", Label: "salvar"}),
					button("Cancelar", extension.VariantGhost, hEditCancel),
					button("Salvar", extension.VariantDefault, hEditSave)),
				editor),
		}
	}
	content := []node{
		extension.Markdown.New(extension.MarkdownProps{Text: d.issue.Description, OnOpen: hLink}),
	}
	return []node{scrolled(content...)}
}

func authorName(u *youtrack.User) string {
	if u == nil {
		return ""
	}
	return firstNonEmpty(u.FullName, u.Login)
}

func commentHeader(c youtrack.Comment, avatars map[string][]byte) node {
	line := muted(authorName(c.Author) + " · " + formatMillis(c.Created))
	if c.Author == nil {
		return line
	}
	return row(avatar(avatars[c.Author.AvatarURL], authorName(c.Author), cardAvatarSize), line)
}

func commentsBody(d *detail, avatars map[string][]byte) []node {
	var thread []node
	for _, c := range d.comments {
		thread = append(thread,
			commentHeader(c, avatars),
			extension.Markdown.New(extension.MarkdownProps{Text: c.Text, OnOpen: hLink}),
			extension.Separator.New(extension.NoProps{}),
		)
	}
	if len(thread) == 0 {
		thread = []node{muted("Nenhum comentário.")}
	}
	composer := extension.Box.New(box{Padding: pad, Gap: gap},
		extension.Textarea.New(extension.TextareaProps{
			Value: d.commentDraft, Placeholder: "Comentar (markdown)", MaxHeight: composerHeight, OnChange: hCommentDraft,
		}).WithKey("comment:"+d.issue.IDReadable),
		row(grow(), extension.KeyHint.New(extension.KeyHintProps{Keys: "ctrl+enter", Label: "enviar"}),
			button("Comentar", extension.VariantDefault, hCommentSend)),
	)
	return []node{scrolled(thread...), extension.Separator.New(extension.NoProps{}), composer}
}

func (a *app) workBody(d *detail) []node {
	var lines []node
	total := 0
	for _, w := range d.work {
		total += w.Duration.Minutes
		kind := ""
		if w.Type != nil {
			kind = " · " + w.Type.Name
		}
		lines = append(lines, extension.TwoLines.New(extension.TwoLinesProps{
			Top:   formatMinutes(w.Duration.Minutes) + kind + " · " + authorName(w.Author),
			Under: strings.TrimSpace(formatDay(w.Date) + "  " + w.Text),
		}))
	}
	if len(lines) == 0 {
		lines = []node{muted("Nenhum tempo apontado.")}
	} else {
		lines = append(lines, extension.Label.New(extension.LabelProps{
			Text: "Total: " + formatMinutes(total), Weight: extension.FontSemibold,
		}))
	}
	kind := "Sem tipo"
	entries := make([]extension.MenuEntry, len(d.workTypes))
	for i, t := range d.workTypes {
		entries[i] = entry(t.Name, i == d.workType, on(hWorkType, i))
		if i == d.workType {
			kind = t.Name
		}
	}
	form := extension.Box.New(box{Padding: pad, Gap: gap},
		row(
			extension.Input.New(extension.InputProps{Value: d.workDate, Placeholder: today, Width: workDateWidth, OnChange: hWorkDate}),
			extension.Input.New(extension.InputProps{Value: d.workDuration, Placeholder: "1h 30m", Width: workTimeWidth, OnChange: hWorkDuration, OnSubmit: hWorkSend}),
			a.choice(hWorkType, kind, selectWidth, entries),
		),
		row(
			grow(extension.Input.New(extension.InputProps{Value: d.workText, Placeholder: "Nota", OnChange: hWorkText, OnSubmit: hWorkSend})),
			button("Apontar", extension.VariantDefault, hWorkSend),
		),
	)
	return []node{scrolled(lines...), extension.Separator.New(extension.NoProps{}), form}
}

func historyBody(d *detail) []node {
	var lines []node
	for _, item := range d.activities {
		lines = append(lines, extension.TwoLines.New(extension.TwoLinesProps{
			Top: activityLine(item), Under: authorName(item.Author) + " · " + formatMillis(item.Timestamp),
		}))
	}
	if len(lines) == 0 {
		lines = []node{muted("Nenhuma alteração registrada.")}
	}
	return []node{scrolled(lines...)}
}

func activityLine(item youtrack.ActivityItem) string {
	if item.Category.ID == youtrack.CategoryIssueCreated {
		return "Criou o card"
	}
	field := item.TargetMember
	if item.Field != nil {
		field = firstNonEmpty(item.Field.Presentation, item.Field.Name, field)
	}
	removed := firstNonEmpty(activityValue(item.Removed), emptyFieldValue)
	added := firstNonEmpty(activityValue(item.Added), emptyFieldValue)
	return fmt.Sprintf("%s: %s → %s", field, removed, added)
}

// createView is a card that does not exist yet, laid out as cardView is.
func (a *app) createView() node {
	c := a.create
	project := "Carregando…"
	entries := make([]extension.MenuEntry, len(c.projects))
	for i, p := range c.projects {
		entries[i] = entry(p.Name, i == c.project, on(hNewProject, i))
		if i == c.project {
			project = p.Name
		}
	}
	header := row(
		extension.GhostIconButton.New(extension.GhostIconButtonProps{Icon: "arrow-left", Hint: "Cancelar", OnPress: hNewCancel}),
		grow(muted("Novo card")),
		extension.KeyHint.New(extension.KeyHintProps{Keys: "ctrl+enter", Label: "criar"}),
		button("Cancelar", extension.VariantGhost, hNewCancel),
		button("Criar", extension.VariantDefault, hNewCreate),
	)
	content := island(extension.Box.New(box{Padding: pad, Gap: gap, Grow: 1},
		header,
		extension.Input.New(extension.InputProps{
			Value: c.summary, Placeholder: "Título: o resultado para o usuário, sem jargão",
			TextSize: summarySize, OnChange: hNewSummary,
		}).WithKey(summaryKey),
		grow(extension.Textarea.New(extension.TextareaProps{
			Value: c.desc, Placeholder: "Descrição (markdown)", Grow: true, OnChange: hNewDesc,
		}).WithKey("new.desc")),
	))
	fields := []node{field("Projeto", a.choice(hNewProject, project, fieldControlWidth, entries))}
	for i, f := range c.schema {
		name, kind := f.Field.Name, f.Field.FieldType.ID
		_, single := singleValueFields[kind]
		_, multi := multiValueFields[kind]
		switch {
		case single:
			chosen := ""
			for _, o := range fieldOptions(f) {
				if o.value == c.values[name] {
					chosen = o.label
				}
			}
			fields = append(fields, field(name,
				a.choice(string(on(hNewField, i)), firstNonEmpty(chosen, projectDefault), fieldControlWidth, fieldEntries(f, chosen, hNewField, i))))
		case multi:
			fields = append(fields, field(name, a.multiChoice(f, c.multi[name], projectDefault, hNewFieldToggle, i)))
		case kind == periodFieldType:
			fields = append(fields, field(name, extension.Input.New(extension.InputProps{
				Value: c.values[name], Placeholder: periodPlaceholder, OnChange: on(hNewFieldText, i),
			}).WithKey("new.period:"+strconv.Itoa(i))))
		}
	}
	side := extension.Box.New(box{Width: fieldsPanelWidth, Background: "sidebar", Radius: islandRadius},
		extension.Scroll.New(extension.ScrollProps{}, extension.Box.New(box{Padding: fieldsPadding, Gap: fieldsGap}, fields...)))
	return extension.Box.New(box{Direction: extension.DirectionRow, Grow: 1, Gap: gutter}, content, side)
}

func (a *app) configDialog() node {
	actions := []node{button("Salvar", extension.VariantDefault, hConfigSave)}
	if a.yt != nil {
		actions = append([]node{button("Cancelar", extension.VariantGhost, hConfigCancel)}, actions...)
	}
	return extension.Dialog.New(extension.DialogProps{
		Open: a.config.open, Title: "Conexão com o YouTrack", MaxWidth: dialogWidth, OnClose: hConfigCancel,
		Description: "Fica salvo só nesta máquina, no diretório de dados da extension.",
		Actions:     actions,
	}, extension.Box.New(box{Gap: gap},
		extension.FormLabel.New(extension.FormLabelProps{Text: "URL da instância"}),
		extension.Input.New(extension.InputProps{
			Value: a.config.url, Placeholder: "https://youtrack.exemplo.com", OnChange: hConfigURL,
		}).WithKey(urlKey),
		extension.FormLabel.New(extension.FormLabelProps{Text: "Token permanente"}),
		extension.Input.New(extension.InputProps{
			Value: a.config.token, Password: true, OnChange: hConfigToken, OnSubmit: hConfigSave,
		}).WithKey("config.token"),
		extension.CheckRow.New(extension.CheckRowProps{
			Checked: a.config.insecure, OnChange: hConfigInsecure,
			Label: "Conexão não segura",
			Note:  "Não verifica o certificado do servidor: quem estiver no caminho pode ler o token.",
		}),
	))
}
