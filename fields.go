package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lichdotdev/lich/extension"

	"github.com/omartelo/lichtrack/youtrack"
)

const (
	periodFieldType   = "period"
	periodPlaceholder = "ex.: 4h 30m"
	projectDefault    = "Padrão do projeto"
)

// multiValueFields are the field types that hold several values out of a set
// the project defines, such as the sprints of a card.
var multiValueFields = map[string]func(field string, values ...string) youtrack.CustomFieldUpdate{
	"enum[*]":       youtrack.MultiEnumField,
	"version[*]":    youtrack.MultiVersionField,
	"build[*]":      youtrack.MultiBuildField,
	"ownedField[*]": youtrack.MultiOwnedField,
	"user[*]":       youtrack.MultiUserField,
}

// valueKeys is what a field of the issue holds, each value spelled as
// fieldOptions spells an option's value.
func valueKeys(issue youtrack.Issue, name string) []string {
	for _, f := range issue.CustomFields {
		if f.Name != name {
			continue
		}
		values, _ := f.Values()
		keys := make([]string, len(values))
		for i, v := range values {
			keys[i] = firstNonEmpty(v.Login, v.Name)
		}
		return keys
	}
	return nil
}

func toggled(values []string, value string) []string {
	if at := slices.Index(values, value); at >= 0 {
		return slices.Delete(slices.Clone(values), at, at+1)
	}
	return append(slices.Clone(values), value)
}

// multiChoice is the select of a field that holds several values: a row
// toggles its value.
func (a *app) multiChoice(f youtrack.ProjectCustomField, chosen []string, empty, handler string, field int) node {
	options := fieldOptions(f)
	entries := make([]extension.MenuEntry, len(options))
	var labels []string
	for i, o := range options {
		checked := slices.Contains(chosen, o.value)
		entries[i] = extension.MenuEntry{Kind: extension.MenuCheckboxItem, Label: o.label, Checked: checked,
			// A menu's checkbox fires OnCheckedChange only; the panel choice draws
			// for a long list presses OnSelect.
			OnSelect: on(handler, field, i), OnCheckedChange: on(handler, field, i)}
		if checked {
			labels = append(labels, o.label)
		}
	}
	return a.choice(string(on(handler, field)), firstNonEmpty(strings.Join(labels, ", "), empty), fieldControlWidth, entries)
}

// toggleField reads "field index:option index" and flips that option of a
// field that holds several values.
func (a *app) toggleField(d *detail, arg string) {
	field, choice, ok := indexPair(arg)
	if !ok || field >= len(d.schema) || choice < 0 {
		return
	}
	f := d.schema[field]
	build, multi := multiValueFields[f.Field.FieldType.ID]
	options := fieldOptions(f)
	if !multi || choice >= len(options) {
		return
	}
	id := d.issue.IDReadable
	update := build(f.Field.Name, toggled(valueKeys(d.issue, f.Field.Name), options[choice].value)...)
	a.updateIssue(id, func(ctx context.Context, yt *youtrack.Client) (youtrack.Issue, error) {
		return yt.SetCustomFields(ctx, id, update)
	})
}

// periodUpdate reads a period as typed; empty clears the field.
func periodUpdate(name, typed string) (youtrack.CustomFieldUpdate, error) {
	if strings.TrimSpace(typed) == "" {
		return youtrack.ClearField(name, youtrack.TypePeriod), nil
	}
	minutes, err := parseDuration(typed)
	if err != nil {
		return youtrack.CustomFieldUpdate{}, err
	}
	return youtrack.PeriodField(name, minutes), nil
}

// setPeriod saves the period typed into the field at index arg.
func (a *app) setPeriod(d *detail, arg, typed string) {
	field, err := strconv.Atoi(arg)
	if err != nil || field < 0 || field >= len(d.schema) || d.schema[field].Field.FieldType.ID != periodFieldType {
		return
	}
	name := d.schema[field].Field.Name
	update, err := periodUpdate(name, typed)
	if err != nil {
		a.fail(name+" inválida", err)
		return
	}
	id := d.issue.IDReadable
	a.updateIssue(id, func(ctx context.Context, yt *youtrack.Client) (youtrack.Issue, error) {
		return yt.SetCustomFields(ctx, id, update)
	})
}

func (a *app) toggleCreateField(arg string) {
	field, choice, ok := indexPair(arg)
	if !ok || field >= len(a.create.schema) || choice < 0 {
		return
	}
	f := a.create.schema[field]
	if options := fieldOptions(f); choice < len(options) {
		a.create.multi[f.Field.Name] = toggled(a.create.multi[f.Field.Name], options[choice].value)
	}
}

func (a *app) typeCreateField(arg, typed string) {
	if field, err := strconv.Atoi(arg); err == nil && field >= 0 && field < len(a.create.schema) {
		a.create.values[a.create.schema[field].Field.Name] = typed
	}
}

// createFields is what the form chose for each field of the project, as the
// updates a new issue carries. A field left alone is left to the project.
func createFields(c createForm) ([]youtrack.CustomFieldUpdate, error) {
	var updates []youtrack.CustomFieldUpdate
	for _, f := range c.schema {
		name, kind := f.Field.Name, f.Field.FieldType.ID
		value := c.values[name]
		if build, single := singleValueFields[kind]; single && value != "" {
			updates = append(updates, build(name, value))
		}
		if build, multi := multiValueFields[kind]; multi && len(c.multi[name]) > 0 {
			updates = append(updates, build(name, c.multi[name]...))
		}
		if kind == periodFieldType && strings.TrimSpace(value) != "" {
			update, err := periodUpdate(name, value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			updates = append(updates, update)
		}
	}
	return updates, nil
}
