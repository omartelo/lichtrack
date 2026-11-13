package youtrack

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	pathWorkItems  = "/timeTracking/workItems"
	workItemFields = "id,text,date,created,updated,duration(minutes,presentation)," +
		"type(" + workItemTypeFields + "),author(" + userFields + "),issue(idReadable,summary)"
	pathAllWorkItems = "/workItems"
	workItemDay      = "2006-01-02"
	// AuthorMe is the author value YouTrack reads as the token's user.
	AuthorMe = "me"

	pathAttachments  = "/attachments"
	attachmentFields = "id,name,size,mimeType,extension,created,url,author(" + userFields + ")"
	// The API takes any form field name, one per file; the docs use "upload1".
	attachmentFormField = "upload1"
)

type Duration struct {
	Minutes      int    `json:"minutes"`
	Presentation string `json:"presentation"`
}

type WorkItem struct {
	ID       string        `json:"id"`
	Text     string        `json:"text"`
	Date     int64         `json:"date"`
	Created  int64         `json:"created"`
	Updated  *int64        `json:"updated"`
	Duration Duration      `json:"duration"`
	Type     *WorkItemType `json:"type"`
	Author   *User         `json:"author"`
	Issue    *struct {
		IDReadable string `json:"idReadable"`
		Summary    string `json:"summary"`
	} `json:"issue"`
}

// WorkItemInput with an empty TypeID sends type null (no type); the author
// is left to the server, which uses the token's user.
type WorkItemInput struct {
	Date    time.Time
	Minutes int
	Text    string
	TypeID  string
}

func (in WorkItemInput) body() any {
	body := struct {
		Date     int64  `json:"date"`
		Text     string `json:"text"`
		Duration struct {
			Minutes int `json:"minutes"`
		} `json:"duration"`
		Type *entityRef `json:"type"`
	}{Date: millis(in.Date), Text: in.Text}
	body.Duration.Minutes = in.Minutes
	if in.TypeID != "" {
		body.Type = &entityRef{in.TypeID}
	}
	return body
}

func workItemPath(issueID, itemID string) string {
	return issuePath(issueID) + pathWorkItems + "/" + segment(itemID)
}

func (c *Client) WorkItems(ctx context.Context, issueID string, page Page) ([]WorkItem, error) {
	var items []WorkItem
	return items, c.get(ctx, issuePath(issueID)+pathWorkItems, pagedQuery(workItemFields, page), &items)
}

// WorkItemsBetween lists what author logged from the day of start to the day
// of end, both included, over every issue.
func (c *Client) WorkItemsBetween(ctx context.Context, author string, start, end time.Time, page Page) ([]WorkItem, error) {
	q := pagedQuery(workItemFields, page)
	q.Set("author", author)
	q.Set("startDate", start.Format(workItemDay))
	q.Set("endDate", end.Format(workItemDay))
	var items []WorkItem
	return items, c.get(ctx, pathAllWorkItems, q, &items)
}

func (c *Client) AddWorkItem(ctx context.Context, issueID string, in WorkItemInput) (WorkItem, error) {
	var item WorkItem
	return item, c.post(ctx, issuePath(issueID)+pathWorkItems, fieldsQuery(workItemFields), in.body(), &item)
}

func (c *Client) UpdateWorkItem(ctx context.Context, issueID, itemID string, in WorkItemInput) (WorkItem, error) {
	var item WorkItem
	return item, c.post(ctx, workItemPath(issueID, itemID), fieldsQuery(workItemFields), in.body(), &item)
}

func (c *Client) DeleteWorkItem(ctx context.Context, issueID, itemID string) error {
	return c.delete(ctx, workItemPath(issueID, itemID))
}

type Attachment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mimeType"`
	Extension string `json:"extension"`
	Created   int64  `json:"created"`
	URL       string `json:"url"`
	Author    *User  `json:"author"`
}

func (c *Client) Attachments(ctx context.Context, issueID string, page Page) ([]Attachment, error) {
	var attachments []Attachment
	return attachments, c.get(ctx, issuePath(issueID)+pathAttachments, pagedQuery(attachmentFields, page), &attachments)
}

// shortcut: the file is buffered in memory to build the multipart body;
// stream through io.Pipe if attachments get large.
func (c *Client) UploadAttachment(ctx context.Context, issueID, fileName string, content io.Reader) (Attachment, error) {
	var attachment Attachment
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	part, err := form.CreateFormFile(attachmentFormField, fileName)
	if err != nil {
		return attachment, fmt.Errorf("youtrack: build upload: %w", err)
	}
	if _, err := io.Copy(part, content); err != nil {
		return attachment, fmt.Errorf("youtrack: read attachment: %w", err)
	}
	if err := form.Close(); err != nil {
		return attachment, fmt.Errorf("youtrack: build upload: %w", err)
	}

	var created []Attachment
	path := issuePath(issueID) + pathAttachments
	if err := c.send(ctx, http.MethodPost, path, fieldsQuery(attachmentFields), &buf, form.FormDataContentType(), &created); err != nil {
		return attachment, err
	}
	if len(created) == 0 {
		return attachment, fmt.Errorf("youtrack: upload of %q returned no attachment", fileName)
	}
	return created[0], nil
}
