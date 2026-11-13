package youtrack

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	pathIssues   = "/issues"
	pathCommands = "/commands"

	userFields  = "id,login,fullName,email,avatarUrl"
	tagFields   = "id,name"
	issueFields = "id,idReadable,summary,description,created,updated,resolved," +
		"project(id,name,shortName),reporter(" + userFields + "),tags(" + tagFields + ")," +
		"customFields(id,name,value(" + customFieldValueFields + "))"
)

type Issue struct {
	ID           string             `json:"id"`
	IDReadable   string             `json:"idReadable"`
	Summary      string             `json:"summary"`
	Description  string             `json:"description"`
	Created      int64              `json:"created"`
	Updated      int64              `json:"updated"`
	Resolved     *int64             `json:"resolved"`
	Project      Project            `json:"project"`
	Reporter     *User              `json:"reporter"`
	Tags         []Tag              `json:"tags"`
	CustomFields []IssueCustomField `json:"customFields"`
}

type NewIssue struct {
	ProjectID    string
	Summary      string
	Description  string
	CustomFields []CustomFieldUpdate
}

type entityRef struct {
	ID string `json:"id"`
}

func issuePath(issueID string) string {
	return pathIssues + "/" + segment(issueID)
}

func (c *Client) SearchIssues(ctx context.Context, query string, page Page) ([]Issue, error) {
	q := pagedQuery(issueFields, page)
	q.Set("query", query)
	var issues []Issue
	return issues, c.get(ctx, pathIssues, q, &issues)
}

// Issue accepts the readable id (ABC-123) or the database id, as does every
// method that takes an issueID.
func (c *Client) Issue(ctx context.Context, issueID string) (Issue, error) {
	var issue Issue
	return issue, c.get(ctx, issuePath(issueID), fieldsQuery(issueFields), &issue)
}

func (c *Client) CreateIssue(ctx context.Context, in NewIssue) (Issue, error) {
	body := struct {
		Project      entityRef           `json:"project"`
		Summary      string              `json:"summary"`
		Description  string              `json:"description,omitempty"`
		CustomFields []CustomFieldUpdate `json:"customFields,omitempty"`
	}{entityRef{in.ProjectID}, in.Summary, in.Description, in.CustomFields}
	var issue Issue
	return issue, c.post(ctx, pathIssues, fieldsQuery(issueFields), body, &issue)
}

func (c *Client) updateIssue(ctx context.Context, issueID string, body any) (Issue, error) {
	var issue Issue
	return issue, c.post(ctx, issuePath(issueID), fieldsQuery(issueFields), body, &issue)
}

func (c *Client) SetSummary(ctx context.Context, issueID, summary string) (Issue, error) {
	return c.updateIssue(ctx, issueID, map[string]string{"summary": summary})
}

func (c *Client) SetDescription(ctx context.Context, issueID, description string) (Issue, error) {
	return c.updateIssue(ctx, issueID, map[string]string{"description": description})
}

func (c *Client) SetCustomFields(ctx context.Context, issueID string, fields ...CustomFieldUpdate) (Issue, error) {
	return c.updateIssue(ctx, issueID, map[string][]CustomFieldUpdate{"customFields": fields})
}

func (c *Client) DeleteIssue(ctx context.Context, issueID string) error {
	return c.delete(ctx, issuePath(issueID))
}

// ApplyCommand takes readable issue ids (ABC-123): they are sent as idReadable.
func (c *Client) ApplyCommand(ctx context.Context, command string, readableIDs ...string) error {
	type issueRef struct {
		IDReadable string `json:"idReadable"`
	}
	body := struct {
		Query  string     `json:"query"`
		Issues []issueRef `json:"issues"`
	}{Query: command}
	for _, id := range readableIDs {
		body.Issues = append(body.Issues, issueRef{id})
	}
	return c.post(ctx, pathCommands, nil, body, nil)
}

const (
	pathTags         = "/tags"
	pathSavedQueries = "/savedQueries"
	savedQueryFields = "id,name,query,pinned"
)

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SavedQuery struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Query string `json:"query"`
	// Pinned is whether the token's user keeps the search in their sidebar.
	Pinned bool `json:"pinned"`
}

func (c *Client) Tags(ctx context.Context, page Page) ([]Tag, error) {
	var tags []Tag
	return tags, c.get(ctx, pathTags, pagedQuery(tagFields, page), &tags)
}

func (c *Client) SavedQueries(ctx context.Context, page Page) ([]SavedQuery, error) {
	var queries []SavedQuery
	return queries, c.get(ctx, pathSavedQueries, pagedQuery(savedQueryFields, page), &queries)
}

func (c *Client) AddTag(ctx context.Context, issueID, tagID string) error {
	return c.post(ctx, issuePath(issueID)+pathTags, nil, entityRef{tagID}, nil)
}

func (c *Client) RemoveTag(ctx context.Context, issueID, tagID string) error {
	return c.delete(ctx, issuePath(issueID)+pathTags+"/"+segment(tagID))
}

const (
	pathComments  = "/comments"
	commentFields = "id,text,created,updated,author(" + userFields + ")"
)

type Comment struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Created int64  `json:"created"`
	Updated *int64 `json:"updated"`
	Author  *User  `json:"author"`
}

type commentBody struct {
	Text string `json:"text"`
}

func commentPath(issueID, commentID string) string {
	return issuePath(issueID) + pathComments + "/" + segment(commentID)
}

func (c *Client) Comments(ctx context.Context, issueID string, page Page) ([]Comment, error) {
	var comments []Comment
	return comments, c.get(ctx, issuePath(issueID)+pathComments, pagedQuery(commentFields, page), &comments)
}

func (c *Client) AddComment(ctx context.Context, issueID, text string) (Comment, error) {
	var comment Comment
	return comment, c.post(ctx, issuePath(issueID)+pathComments, fieldsQuery(commentFields), commentBody{text}, &comment)
}

func (c *Client) UpdateComment(ctx context.Context, issueID, commentID, text string) (Comment, error) {
	var comment Comment
	return comment, c.post(ctx, commentPath(issueID, commentID), fieldsQuery(commentFields), commentBody{text}, &comment)
}

func (c *Client) DeleteComment(ctx context.Context, issueID, commentID string) error {
	return c.delete(ctx, commentPath(issueID, commentID))
}

const (
	pathLinks      = "/links"
	pathLinkTypes  = "/issueLinkTypes"
	linkTypeFields = "id,name,sourceToTarget,targetToSource,directed"
	linkFields     = "id,direction,linkType(" + linkTypeFields + "),issues(id,idReadable,summary,resolved)"
)

type LinkType struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SourceToTarget string `json:"sourceToTarget"`
	TargetToSource string `json:"targetToSource"`
	Directed       bool   `json:"directed"`
}

// IssueLink.ID is the linkID AddLink and RemoveLink expect: the link type id
// plus "s" (outward) or "t" (inward) for directed types. Links returns one
// entry per type and direction, including the ones with no issues.
type IssueLink struct {
	ID        string   `json:"id"`
	Direction string   `json:"direction"`
	LinkType  LinkType `json:"linkType"`
	Issues    []Issue  `json:"issues"`
}

func linkIssuesPath(issueID, linkID string) string {
	return issuePath(issueID) + pathLinks + "/" + segment(linkID) + pathIssues
}

func (c *Client) LinkTypes(ctx context.Context, page Page) ([]LinkType, error) {
	var types []LinkType
	return types, c.get(ctx, pathLinkTypes, pagedQuery(linkTypeFields, page), &types)
}

func (c *Client) Links(ctx context.Context, issueID string) ([]IssueLink, error) {
	var links []IssueLink
	return links, c.get(ctx, issuePath(issueID)+pathLinks, fieldsQuery(linkFields), &links)
}

// AddLink and RemoveLink take the target's database id (Issue.ID), the only
// form the docs show for the linked issue.
func (c *Client) AddLink(ctx context.Context, issueID, linkID, targetDatabaseID string) error {
	return c.post(ctx, linkIssuesPath(issueID, linkID), nil, entityRef{targetDatabaseID}, nil)
}

func (c *Client) RemoveLink(ctx context.Context, issueID, linkID, targetDatabaseID string) error {
	return c.delete(ctx, linkIssuesPath(issueID, linkID)+"/"+segment(targetDatabaseID))
}

const (
	pathActivities = "/activities"
	activityValue  = "id,name,login,fullName,text,idReadable,summary,presentation,minutes"
	activityFields = "id,timestamp,targetMember,author(" + userFields + "),category(id)," +
		"field(id,name,presentation),added(" + activityValue + "),removed(" + activityValue + ")"

	CategoryComments     = "CommentsCategory"
	CategoryCustomField  = "CustomFieldCategory"
	CategorySummary      = "SummaryCategory"
	CategoryDescription  = "DescriptionCategory"
	CategoryIssueCreated = "IssueCreatedCategory"
	CategoryResolved     = "IssueResolvedCategory"
	CategoryLinks        = "LinksCategory"
	CategoryTags         = "TagsCategory"
	CategoryAttachments  = "AttachmentsCategory"
	CategorySprint       = "SprintCategory"
	CategoryWorkItem     = "WorkItemCategory"
	CategoryProject      = "ProjectCategory"
)

// Added and Removed stay raw: depending on the category they hold a string,
// an entity or a list of entities.
type ActivityItem struct {
	ID           string `json:"id"`
	Timestamp    int64  `json:"timestamp"`
	TargetMember string `json:"targetMember"`
	Author       *User  `json:"author"`
	Category     struct {
		ID string `json:"id"`
	} `json:"category"`
	Field *struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Presentation string `json:"presentation"`
	} `json:"field"`
	Added   json.RawMessage `json:"added"`
	Removed json.RawMessage `json:"removed"`
}

// Activities requires at least one category: the API rejects the request
// without the categories parameter.
func (c *Client) Activities(ctx context.Context, issueID string, categories []string, page Page) ([]ActivityItem, error) {
	q := pagedQuery(activityFields, page)
	q.Set("categories", strings.Join(categories, ","))
	var items []ActivityItem
	return items, c.get(ctx, issuePath(issueID)+pathActivities, q, &items)
}
