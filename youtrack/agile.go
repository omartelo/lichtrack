package youtrack

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

const (
	pathAgiles      = "/agiles"
	pathSprints     = "/sprints"
	currentSprintID = "current"

	sprintFields = "id,name,goal,start,finish,archived,isDefault,unresolvedIssuesCount"
	agileFields  = "id,name,favorite,projects(" + projectFields + "),currentSprint(" + sprintFields + ")," +
		"columnSettings(field(id,name,fieldType(id,valueType,isMultiValue))," +
		"columns(id,presentation,isResolved,ordinal,fieldValues(id,name,isResolved)))"
	sprintIssuesFields = sprintFields + ",issues(" + issueFields + ")"

	commandBoard = "Board"
)

type Sprint struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Goal                  string `json:"goal"`
	Start                 *int64 `json:"start"`
	Finish                *int64 `json:"finish"`
	Archived              bool   `json:"archived"`
	IsDefault             bool   `json:"isDefault"`
	UnresolvedIssuesCount int    `json:"unresolvedIssuesCount"`
}

type AgileColumn struct {
	ID           string `json:"id"`
	Presentation string `json:"presentation"`
	IsResolved   bool   `json:"isResolved"`
	Ordinal      int    `json:"ordinal"`
	FieldValues  []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		IsResolved bool   `json:"isResolved"`
	} `json:"fieldValues"`
}

type Agile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Favorite is the star the token's user gave the board.
	Favorite       bool      `json:"favorite"`
	Projects       []Project `json:"projects"`
	CurrentSprint  *Sprint   `json:"currentSprint"`
	ColumnSettings struct {
		Field   *CustomField  `json:"field"`
		Columns []AgileColumn `json:"columns"`
	} `json:"columnSettings"`
}

type BoardColumn struct {
	Column AgileColumn
	Issues []Issue
}

// Unmatched holds the sprint issues whose column-field value belongs to no
// column of the board.
type SprintBoard struct {
	Agile     Agile
	Sprint    Sprint
	Columns   []BoardColumn
	Unmatched []Issue
}

func agilePath(boardID string) string {
	return pathAgiles + "/" + segment(boardID)
}

func sprintPath(boardID, sprintID string) string {
	return agilePath(boardID) + pathSprints + "/" + segment(sprintID)
}

func (c *Client) Boards(ctx context.Context, page Page) ([]Agile, error) {
	var boards []Agile
	return boards, c.get(ctx, pathAgiles, pagedQuery(agileFields, page), &boards)
}

func (c *Client) Board(ctx context.Context, boardID string) (Agile, error) {
	var board Agile
	return board, c.get(ctx, agilePath(boardID), fieldsQuery(agileFields), &board)
}

func (c *Client) Sprints(ctx context.Context, boardID string, page Page) ([]Sprint, error) {
	var sprints []Sprint
	return sprints, c.get(ctx, agilePath(boardID)+pathSprints, pagedQuery(sprintFields, page), &sprints)
}

func (c *Client) CurrentSprint(ctx context.Context, boardID string) (Sprint, error) {
	var sprint Sprint
	return sprint, c.get(ctx, sprintPath(boardID, currentSprintID), fieldsQuery(sprintFields), &sprint)
}

// SprintBoard assembles the board on the client: the REST API exposes no
// resource with the cards already split by column, so the sprint issues are
// grouped by their value of the board's column field.
func (c *Client) SprintBoard(ctx context.Context, boardID, sprintID string) (SprintBoard, error) {
	var board SprintBoard
	agile, err := c.Board(ctx, boardID)
	if err != nil {
		return board, err
	}
	var sprint struct {
		Sprint
		Issues []Issue `json:"issues"`
	}
	if err := c.get(ctx, sprintPath(boardID, sprintID), fieldsQuery(sprintIssuesFields), &sprint); err != nil {
		return board, err
	}

	// The API lists the columns in no order; ordinal is where each stands.
	slices.SortStableFunc(agile.ColumnSettings.Columns, func(a, b AgileColumn) int { return cmp.Compare(a.Ordinal, b.Ordinal) })
	board.Agile = agile
	board.Sprint = sprint.Sprint
	columnByValue := map[string]int{}
	for i, column := range agile.ColumnSettings.Columns {
		board.Columns = append(board.Columns, BoardColumn{Column: column})
		for _, value := range column.FieldValues {
			columnByValue[value.Name] = i
		}
	}
	for _, issue := range sprint.Issues {
		value, err := columnValue(issue, agile.ColumnSettings.Field)
		if err != nil {
			return board, err
		}
		if i, ok := columnByValue[value]; ok && value != "" {
			board.Columns[i].Issues = append(board.Columns[i].Issues, issue)
			continue
		}
		board.Unmatched = append(board.Unmatched, issue)
	}
	return board, nil
}

func columnValue(issue Issue, field *CustomField) (string, error) {
	if field == nil {
		return "", nil
	}
	for _, f := range issue.CustomFields {
		if f.Name != field.Name {
			continue
		}
		values, err := f.Values()
		if err != nil || len(values) == 0 {
			return "", err
		}
		return values[0].Name, nil
	}
	return "", nil
}

// MoveCardToColumn sets the board's column field to the column's first value,
// which is what dragging a card does.
func (c *Client) MoveCardToColumn(ctx context.Context, issueID string, board Agile, column AgileColumn) (Issue, error) {
	field := board.ColumnSettings.Field
	if field == nil {
		return Issue{}, fmt.Errorf("youtrack: board %q has no column field", board.Name)
	}
	if len(column.FieldValues) == 0 {
		return Issue{}, fmt.Errorf("youtrack: column %q has no field value", column.Presentation)
	}
	fieldType, err := IssueFieldType(field.FieldType.ID)
	if err != nil {
		return Issue{}, err
	}
	return c.SetCustomFields(ctx, issueID, CustomFieldUpdate{
		Name:  field.Name,
		Type:  fieldType,
		Value: namedValue{column.FieldValues[0].Name},
	})
}

// The REST API documents no endpoint to change the sprint of an issue; the
// documented way is the "Board <board> <sprint>" command.
func (c *Client) MoveCardToSprint(ctx context.Context, readableID, boardName, sprintName string) error {
	return c.ApplyCommand(ctx, commandBoard+" "+boardName+" "+sprintName, readableID)
}

func (c *Client) RemoveCardFromSprint(ctx context.Context, readableID, boardName, sprintName string) error {
	return c.ApplyCommand(ctx, "remove "+commandBoard+" "+boardName+" "+sprintName, readableID)
}
