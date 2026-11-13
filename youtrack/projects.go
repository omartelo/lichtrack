package youtrack

import "context"

const (
	pathUsers         = "/users"
	pathMe            = "/users/me"
	pathProjects      = "/admin/projects"
	pathCustomFields  = "/customFields"
	pathWorkItemTypes = "/timeTrackingSettings/workItemTypes"

	projectFields      = "id,name,shortName,archived"
	workItemTypeFields = "id,name"
	// aggregatedUsers exists only on user bundles, values on the others.
	projectCustomFieldFields = "id,canBeEmpty,emptyFieldText,field(id,name,fieldType(id,valueType,isMultiValue))," +
		"bundle(id,values(id,name,description,archived,isResolved,released),aggregatedUsers(" + userFields + "))"
)

type User struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	FullName  string `json:"fullName"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl"`
}

type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
	Archived  bool   `json:"archived"`
}

type FieldType struct {
	ID           string `json:"id"`
	ValueType    string `json:"valueType"`
	IsMultiValue bool   `json:"isMultiValue"`
}

type CustomField struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	FieldType FieldType `json:"fieldType"`
}

type BundleValue struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
	IsResolved  bool   `json:"isResolved"`
	Released    bool   `json:"released"`
}

// Bundle is nil for fields without a value set (string, number, date,
// period, text); user fields fill Users, the other bundles fill Values.
type ProjectCustomField struct {
	ID             string      `json:"id"`
	Type           string      `json:"$type"`
	CanBeEmpty     bool        `json:"canBeEmpty"`
	EmptyFieldText string      `json:"emptyFieldText"`
	Field          CustomField `json:"field"`
	Bundle         *struct {
		ID     string        `json:"id"`
		Values []BundleValue `json:"values"`
		Users  []User        `json:"aggregatedUsers"`
	} `json:"bundle"`
}

type WorkItemType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) Me(ctx context.Context) (User, error) {
	var user User
	return user, c.get(ctx, pathMe, fieldsQuery(userFields), &user)
}

func (c *Client) SearchUsers(ctx context.Context, query string, page Page) ([]User, error) {
	q := pagedQuery(userFields, page)
	q.Set("query", query)
	var users []User
	return users, c.get(ctx, pathUsers, q, &users)
}

func (c *Client) Projects(ctx context.Context, page Page) ([]Project, error) {
	var projects []Project
	return projects, c.get(ctx, pathProjects, pagedQuery(projectFields, page), &projects)
}

func (c *Client) ProjectCustomFields(ctx context.Context, projectID string, page Page) ([]ProjectCustomField, error) {
	var fields []ProjectCustomField
	path := pathProjects + "/" + segment(projectID) + pathCustomFields
	return fields, c.get(ctx, path, pagedQuery(projectCustomFieldFields, page), &fields)
}

func (c *Client) WorkItemTypes(ctx context.Context, projectID string, page Page) ([]WorkItemType, error) {
	var types []WorkItemType
	path := pathProjects + "/" + segment(projectID) + pathWorkItemTypes
	return types, c.get(ctx, path, pagedQuery(workItemTypeFields, page), &types)
}
