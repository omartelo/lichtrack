package youtrack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testToken = "perm:test-token"

type recorded struct {
	Method      string
	Path        string
	Query       url.Values
	Body        string
	Auth        string
	ContentType string
	FileName    string
	FileContent string
}

type reply struct {
	status int
	body   string
}

// fakeYouTrack answers each request with the next reply and records what
// the client sent.
func fakeYouTrack(t *testing.T, replies ...reply) (*Client, *[]recorded) {
	t.Helper()
	var seen []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recorded{
			Method:      r.Method,
			Path:        r.URL.EscapedPath(),
			Query:       r.URL.Query(),
			Auth:        r.Header.Get("Authorization"),
			ContentType: r.Header.Get("Content-Type"),
		}
		if strings.HasPrefix(rec.ContentType, "multipart/form-data") {
			file, header, err := r.FormFile(attachmentFormField)
			if err != nil {
				t.Errorf("multipart file: %v", err)
			} else {
				content, _ := io.ReadAll(file)
				rec.FileName, rec.FileContent = header.Filename, string(content)
			}
		} else {
			raw, _ := io.ReadAll(r.Body)
			rec.Body = string(raw)
		}
		seen = append(seen, rec)
		if len(seen) > len(replies) {
			t.Errorf("unexpected request %d: %s %s", len(seen), r.Method, r.URL)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		rp := replies[len(seen)-1]
		w.Header().Set("Content-Type", contentTypeJSON)
		w.WriteHeader(rp.status)
		io.WriteString(w, rp.body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", testToken), &seen
}

func ok(body string) reply { return reply{http.StatusOK, body} }

func assertJSON(t *testing.T, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("got is not JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %v\n%s", err, want)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("body mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestEndpoints(t *testing.T) {
	ctx := context.Background()
	day := time.UnixMilli(1539000000000)
	page := Page{Skip: 10, Top: 5}

	cases := []struct {
		name      string
		call      func(c *Client) error
		method    string
		path      string
		query     map[string]string
		body      string
		paginated bool
		response  string
	}{
		{name: "me", call: func(c *Client) error { _, err := c.Me(ctx); return err },
			method: "GET", path: "/api/users/me", response: `{}`},
		{name: "search users", call: func(c *Client) error { _, err := c.SearchUsers(ctx, "jane", page); return err },
			method: "GET", path: "/api/users", query: map[string]string{"query": "jane"}, paginated: true, response: `[]`},
		{name: "projects", call: func(c *Client) error { _, err := c.Projects(ctx, page); return err },
			method: "GET", path: "/api/admin/projects", paginated: true, response: `[]`},
		{name: "project custom fields", call: func(c *Client) error { _, err := c.ProjectCustomFields(ctx, "0-0", page); return err },
			method: "GET", path: "/api/admin/projects/0-0/customFields", paginated: true, response: `[]`},
		{name: "work item types", call: func(c *Client) error { _, err := c.WorkItemTypes(ctx, "0-0", page); return err },
			method: "GET", path: "/api/admin/projects/0-0/timeTrackingSettings/workItemTypes", paginated: true, response: `[]`},

		{name: "search issues", call: func(c *Client) error { _, err := c.SearchIssues(ctx, "for: me #Unresolved", page); return err },
			method: "GET", path: "/api/issues", query: map[string]string{"query": "for: me #Unresolved"}, paginated: true, response: `[]`},
		{name: "issue", call: func(c *Client) error { _, err := c.Issue(ctx, "ABC-123"); return err },
			method: "GET", path: "/api/issues/ABC-123", response: `{}`},
		{name: "create issue", call: func(c *Client) error {
			_, err := c.CreateIssue(ctx, NewIssue{ProjectID: "0-0", Summary: "S", Description: "D",
				CustomFields: []CustomFieldUpdate{EnumField("Priority", "Critical")}})
			return err
		}, method: "POST", path: "/api/issues", response: `{}`,
			body: `{"project":{"id":"0-0"},"summary":"S","description":"D","customFields":[{"name":"Priority","$type":"SingleEnumIssueCustomField","value":{"name":"Critical"}}]}`},
		{name: "set summary", call: func(c *Client) error { _, err := c.SetSummary(ctx, "ABC-1", "new"); return err },
			method: "POST", path: "/api/issues/ABC-1", body: `{"summary":"new"}`, response: `{}`},
		{name: "set description", call: func(c *Client) error { _, err := c.SetDescription(ctx, "ABC-1", ""); return err },
			method: "POST", path: "/api/issues/ABC-1", body: `{"description":""}`, response: `{}`},
		{name: "set custom fields", call: func(c *Client) error {
			_, err := c.SetCustomFields(ctx, "ABC-1", StateField("State", "In Progress"), UserField("Assignee", "jane.doe"))
			return err
		}, method: "POST", path: "/api/issues/ABC-1", response: `{}`,
			body: `{"customFields":[{"name":"State","$type":"StateIssueCustomField","value":{"name":"In Progress"}},{"name":"Assignee","$type":"SingleUserIssueCustomField","value":{"login":"jane.doe"}}]}`},
		{name: "delete issue", call: func(c *Client) error { return c.DeleteIssue(ctx, "ABC-1") },
			method: "DELETE", path: "/api/issues/ABC-1"},

		{name: "command", call: func(c *Client) error { return c.ApplyCommand(ctx, "for me", "ABC-1", "ABC-2") },
			method: "POST", path: "/api/commands", body: `{"query":"for me","issues":[{"idReadable":"ABC-1"},{"idReadable":"ABC-2"}]}`},

		{name: "tags", call: func(c *Client) error { _, err := c.Tags(ctx, page); return err },
			method: "GET", path: "/api/tags", paginated: true, response: `[]`},
		{name: "saved queries", call: func(c *Client) error { _, err := c.SavedQueries(ctx, page); return err },
			method: "GET", path: "/api/savedQueries", paginated: true, response: `[]`},
		{name: "add tag", call: func(c *Client) error { return c.AddTag(ctx, "ABC-1", "6-4") },
			method: "POST", path: "/api/issues/ABC-1/tags", body: `{"id":"6-4"}`, response: `{}`},
		{name: "remove tag", call: func(c *Client) error { return c.RemoveTag(ctx, "ABC-1", "6-4") },
			method: "DELETE", path: "/api/issues/ABC-1/tags/6-4"},

		{name: "comments", call: func(c *Client) error { _, err := c.Comments(ctx, "ABC-1", page); return err },
			method: "GET", path: "/api/issues/ABC-1/comments", paginated: true, response: `[]`},
		{name: "add comment", call: func(c *Client) error { _, err := c.AddComment(ctx, "ABC-1", "hi"); return err },
			method: "POST", path: "/api/issues/ABC-1/comments", body: `{"text":"hi"}`, response: `{}`},
		{name: "update comment", call: func(c *Client) error { _, err := c.UpdateComment(ctx, "ABC-1", "4-7", "edit"); return err },
			method: "POST", path: "/api/issues/ABC-1/comments/4-7", body: `{"text":"edit"}`, response: `{}`},
		{name: "delete comment", call: func(c *Client) error { return c.DeleteComment(ctx, "ABC-1", "4-7") },
			method: "DELETE", path: "/api/issues/ABC-1/comments/4-7"},

		{name: "work items", call: func(c *Client) error { _, err := c.WorkItems(ctx, "ABC-1", page); return err },
			method: "GET", path: "/api/issues/ABC-1/timeTracking/workItems", paginated: true, response: `[]`},
		{name: "add work item", call: func(c *Client) error {
			_, err := c.AddWorkItem(ctx, "ABC-1", WorkItemInput{Date: day, Minutes: 120, Text: "testing", TypeID: "49-0"})
			return err
		}, method: "POST", path: "/api/issues/ABC-1/timeTracking/workItems", response: `{}`,
			body: `{"date":1539000000000,"text":"testing","duration":{"minutes":120},"type":{"id":"49-0"}}`},
		{name: "update work item without type", call: func(c *Client) error {
			_, err := c.UpdateWorkItem(ctx, "ABC-1", "115-0", WorkItemInput{Date: day, Minutes: 30})
			return err
		}, method: "POST", path: "/api/issues/ABC-1/timeTracking/workItems/115-0", response: `{}`,
			body: `{"date":1539000000000,"text":"","duration":{"minutes":30},"type":null}`},
		{name: "delete work item", call: func(c *Client) error { return c.DeleteWorkItem(ctx, "ABC-1", "115-0") },
			method: "DELETE", path: "/api/issues/ABC-1/timeTracking/workItems/115-0"},

		{name: "link types", call: func(c *Client) error { _, err := c.LinkTypes(ctx, page); return err },
			method: "GET", path: "/api/issueLinkTypes", paginated: true, response: `[]`},
		{name: "links", call: func(c *Client) error { _, err := c.Links(ctx, "ABC-1"); return err },
			method: "GET", path: "/api/issues/ABC-1/links", response: `[]`},
		{name: "add link", call: func(c *Client) error { return c.AddLink(ctx, "ABC-1", "80-1s", "2-72") },
			method: "POST", path: "/api/issues/ABC-1/links/80-1s/issues", body: `{"id":"2-72"}`, response: `{}`},
		{name: "remove link", call: func(c *Client) error { return c.RemoveLink(ctx, "ABC-1", "80-1s", "2-72") },
			method: "DELETE", path: "/api/issues/ABC-1/links/80-1s/issues/2-72"},

		{name: "attachments", call: func(c *Client) error { _, err := c.Attachments(ctx, "ABC-1", page); return err },
			method: "GET", path: "/api/issues/ABC-1/attachments", paginated: true, response: `[]`},
		{name: "activities", call: func(c *Client) error {
			_, err := c.Activities(ctx, "ABC-1", []string{CategoryComments, CategoryCustomField}, page)
			return err
		}, method: "GET", path: "/api/issues/ABC-1/activities", paginated: true, response: `[]`,
			query: map[string]string{"categories": "CommentsCategory,CustomFieldCategory"}},

		{name: "boards", call: func(c *Client) error { _, err := c.Boards(ctx, page); return err },
			method: "GET", path: "/api/agiles", paginated: true, response: `[]`},
		{name: "board", call: func(c *Client) error { _, err := c.Board(ctx, "108-4"); return err },
			method: "GET", path: "/api/agiles/108-4", response: `{}`},
		{name: "sprints", call: func(c *Client) error { _, err := c.Sprints(ctx, "108-4", page); return err },
			method: "GET", path: "/api/agiles/108-4/sprints", paginated: true, response: `[]`},
		{name: "current sprint", call: func(c *Client) error { _, err := c.CurrentSprint(ctx, "108-4"); return err },
			method: "GET", path: "/api/agiles/108-4/sprints/current", response: `{}`},
		{name: "move card to sprint", call: func(c *Client) error { return c.MoveCardToSprint(ctx, "ABC-1", "Team Board", "Sprint 5") },
			method: "POST", path: "/api/commands", body: `{"query":"Board Team Board Sprint 5","issues":[{"idReadable":"ABC-1"}]}`},
		{name: "remove card from sprint", call: func(c *Client) error { return c.RemoveCardFromSprint(ctx, "ABC-1", "Team Board", "Sprint 5") },
			method: "POST", path: "/api/commands", body: `{"query":"remove Board Team Board Sprint 5","issues":[{"idReadable":"ABC-1"}]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, seen := fakeYouTrack(t, ok(tc.response))
			if err := tc.call(client); err != nil {
				t.Fatalf("call: %v", err)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests = %d, want 1", len(*seen))
			}
			req := (*seen)[0]
			if req.Method != tc.method || req.Path != tc.path {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tc.method, tc.path)
			}
			if req.Auth != "Bearer "+testToken {
				t.Errorf("Authorization = %q", req.Auth)
			}
			for key, want := range tc.query {
				if got := req.Query.Get(key); got != want {
					t.Errorf("query %s = %q, want %q", key, got, want)
				}
			}
			if tc.paginated && (req.Query.Get("$skip") != "10" || req.Query.Get("$top") != "5") {
				t.Errorf("pagination = $skip %q $top %q, want 10 and 5", req.Query.Get("$skip"), req.Query.Get("$top"))
			}
			if tc.method == "GET" && req.Query.Get("fields") == "" {
				t.Error("fields parameter missing: YouTrack would return only $type")
			}
			if tc.body != "" {
				if req.ContentType != contentTypeJSON {
					t.Errorf("Content-Type = %q", req.ContentType)
				}
				assertJSON(t, req.Body, tc.body)
			}
		})
	}
}

func TestPageDefaultsToServerPageSize(t *testing.T) {
	client, seen := fakeYouTrack(t, ok(`[]`))
	if _, err := client.SearchIssues(context.Background(), "", Page{}); err != nil {
		t.Fatal(err)
	}
	q := (*seen)[0].Query
	if q.Get("$top") != "42" || q.Get("$skip") != "0" {
		t.Errorf("$top = %q, $skip = %q; want 42 and 0", q.Get("$top"), q.Get("$skip"))
	}
}

func TestPaginationWalksPages(t *testing.T) {
	client, seen := fakeYouTrack(t,
		ok(`[{"idReadable":"A-1"},{"idReadable":"A-2"}]`),
		ok(`[{"idReadable":"A-3"}]`),
	)
	var all []string
	for skip := 0; ; skip += 2 {
		issues, err := client.SearchIssues(context.Background(), "project: A", Page{Skip: skip, Top: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, issue := range issues {
			all = append(all, issue.IDReadable)
		}
		if len(issues) < 2 {
			break
		}
	}
	if want := []string{"A-1", "A-2", "A-3"}; !reflect.DeepEqual(all, want) {
		t.Errorf("issues = %v, want %v", all, want)
	}
	if got := (*seen)[1].Query.Get("$skip"); got != "2" {
		t.Errorf("second page $skip = %q, want 2", got)
	}
}

func TestAPIErrors(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		status  int
		code    string
		message string
	}{
		{"unauthorized", reply{401, `{"error":"Unauthorized","error_description":"You are not logged in."}`},
			401, "Unauthorized", "You are not logged in."},
		{"not found", reply{404, `{"error":"Not Found","error_description":"Entity with id ABC-999 not found"}`},
			404, "Not Found", "Entity with id ABC-999 not found"},
		{"bad request", reply{400, `{"error":"bad_request","error_description":"Unknown field value: Bogus"}`},
			400, "bad_request", "Unknown field value: Bogus"},
		{"non json body", reply{502, "<html>Bad Gateway</html>"}, 502, "", "<html>Bad Gateway</html>"},
		{"empty body", reply{503, ""}, 503, "", "Service Unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := fakeYouTrack(t, tc.reply)
			_, err := client.Issue(context.Background(), "ABC-999")
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want *APIError", err)
			}
			if apiErr.Status != tc.status || apiErr.Code != tc.code || apiErr.Message != tc.message {
				t.Errorf("error = %+v, want status %d code %q message %q", apiErr, tc.status, tc.code, tc.message)
			}
			if !strings.Contains(err.Error(), tc.message) {
				t.Errorf("Error() = %q, want it to carry %q", err.Error(), tc.message)
			}
		})
	}
}

func TestErrorOnWriteAndDelete(t *testing.T) {
	notFound := reply{404, `{"error":"Not Found","error_description":"Entity with id 6-4 not found"}`}
	client, _ := fakeYouTrack(t, notFound, notFound)
	for _, err := range []error{
		client.AddTag(context.Background(), "ABC-1", "6-4"),
		client.DeleteIssue(context.Background(), "ABC-1"),
	} {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			t.Errorf("error = %v, want 404 APIError", err)
		}
	}
}

func TestInvalidJSONResponse(t *testing.T) {
	client, _ := fakeYouTrack(t, ok(`{not json`))
	_, err := client.Me(context.Background())
	var apiErr *APIError
	if err == nil || errors.As(err, &apiErr) {
		t.Errorf("error = %v, want a decode error", err)
	}
}

func TestCanceledContext(t *testing.T) {
	client, seen := fakeYouTrack(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Me(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if len(*seen) != 0 {
		t.Errorf("server got %d requests", len(*seen))
	}
}

func TestPathSegmentsAreEscaped(t *testing.T) {
	client, seen := fakeYouTrack(t, ok(`{}`))
	if _, err := client.Issue(context.Background(), "a/b?c"); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0].Path; got != "/api/issues/a%2Fb%3Fc" {
		t.Errorf("path = %q", got)
	}
}

func TestCustomFieldSerialization(t *testing.T) {
	at := time.UnixMilli(1700000000000)
	cases := []struct {
		name  string
		field CustomFieldUpdate
		want  string
	}{
		{"enum", EnumField("Priority", "Major"), `{"name":"Priority","$type":"SingleEnumIssueCustomField","value":{"name":"Major"}}`},
		{"multi enum", MultiEnumField("Labels", "a", "b"), `{"name":"Labels","$type":"MultiEnumIssueCustomField","value":[{"name":"a"},{"name":"b"}]}`},
		{"multi enum empty", MultiEnumField("Labels"), `{"name":"Labels","$type":"MultiEnumIssueCustomField","value":[]}`},
		{"state", StateField("State", "Open"), `{"name":"State","$type":"StateIssueCustomField","value":{"name":"Open"}}`},
		{"user", UserField("Assignee", "jane.doe"), `{"name":"Assignee","$type":"SingleUserIssueCustomField","value":{"login":"jane.doe"}}`},
		{"multi user", MultiUserField("Reviewers", "a", "b"), `{"name":"Reviewers","$type":"MultiUserIssueCustomField","value":[{"login":"a"},{"login":"b"}]}`},
		{"version", VersionField("Fix version", "2019.1"), `{"name":"Fix version","$type":"SingleVersionIssueCustomField","value":{"name":"2019.1"}}`},
		{"multi version", MultiVersionField("Fix versions", "2019.1"), `{"name":"Fix versions","$type":"MultiVersionIssueCustomField","value":[{"name":"2019.1"}]}`},
		{"build", BuildField("Fixed in build", "42"), `{"name":"Fixed in build","$type":"SingleBuildIssueCustomField","value":{"name":"42"}}`},
		{"multi build", MultiBuildField("Builds", "42"), `{"name":"Builds","$type":"MultiBuildIssueCustomField","value":[{"name":"42"}]}`},
		{"owned", OwnedField("Subsystem", "UI"), `{"name":"Subsystem","$type":"SingleOwnedIssueCustomField","value":{"name":"UI"}}`},
		{"multi owned", MultiOwnedField("Subsystems", "UI"), `{"name":"Subsystems","$type":"MultiOwnedIssueCustomField","value":[{"name":"UI"}]}`},
		{"group", GroupField("Team", "devs"), `{"name":"Team","$type":"SingleGroupIssueCustomField","value":{"name":"devs"}}`},
		{"multi group", MultiGroupField("Teams", "devs"), `{"name":"Teams","$type":"MultiGroupIssueCustomField","value":[{"name":"devs"}]}`},
		{"period", PeriodField("Estimation", 90), `{"name":"Estimation","$type":"PeriodIssueCustomField","value":{"minutes":90}}`},
		{"date", DateField("Due Date", at), `{"name":"Due Date","$type":"DateIssueCustomField","value":1700000000000}`},
		{"date and time", DateTimeField("Deploy at", at), `{"name":"Deploy at","$type":"SimpleIssueCustomField","value":1700000000000}`},
		{"text", TextField("Customer", "Valued"), `{"name":"Customer","$type":"TextIssueCustomField","value":{"text":"Valued"}}`},
		{"string", StringField("Email", "a@b.c"), `{"name":"Email","$type":"SimpleIssueCustomField","value":"a@b.c"}`},
		{"int", IntField("Points", 3), `{"name":"Points","$type":"SimpleIssueCustomField","value":3}`},
		{"float", FloatField("Score", 1.5), `{"name":"Score","$type":"SimpleIssueCustomField","value":1.5}`},
		{"clear", ClearField("Assignee", TypeSingleUser), `{"name":"Assignee","$type":"SingleUserIssueCustomField","value":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.field)
			if err != nil {
				t.Fatal(err)
			}
			assertJSON(t, string(raw), tc.want)
		})
	}
}

func TestIssueFieldType(t *testing.T) {
	for id, want := range map[string]string{
		"state[1]": TypeState, "enum[*]": TypeMultiEnum, "user[1]": TypeSingleUser,
		"period": TypePeriod, "date": TypeDate, "date and time": TypeSimple, "text": TypeText,
	} {
		if got, err := IssueFieldType(id); err != nil || got != want {
			t.Errorf("IssueFieldType(%q) = %q, %v; want %q", id, got, err, want)
		}
	}
	if _, err := IssueFieldType("hologram"); err == nil {
		t.Error("unknown field type: want error")
	}
}

func TestIssueDecodesCustomFieldValues(t *testing.T) {
	client, _ := fakeYouTrack(t, ok(`{
		"id":"2-7","idReadable":"ABC-7","summary":"Fix it","resolved":null,
		"project":{"id":"0-0","shortName":"ABC"},
		"tags":[{"id":"6-1","name":"Star"}],
		"customFields":[
			{"id":"92-1","name":"State","$type":"StateIssueCustomField","value":{"id":"67-2","name":"Open","isResolved":false}},
			{"id":"94-0","name":"Assignee","$type":"SingleUserIssueCustomField","value":{"login":"jane.doe","fullName":"Jane Doe"}},
			{"id":"95-0","name":"Fix versions","$type":"MultiVersionIssueCustomField","value":[{"name":"1.0"},{"name":"1.1"}]},
			{"id":"96-0","name":"Estimation","$type":"PeriodIssueCustomField","value":{"minutes":90,"presentation":"1h 30m"}},
			{"id":"97-0","name":"Notes","$type":"TextIssueCustomField","value":{"text":"hello"}},
			{"id":"98-0","name":"Due Date","$type":"DateIssueCustomField","value":1700000000000},
			{"id":"99-0","name":"Subsystem","$type":"SingleOwnedIssueCustomField","value":null}
		]}`))
	issue, err := client.Issue(context.Background(), "ABC-7")
	if err != nil {
		t.Fatal(err)
	}
	if issue.IDReadable != "ABC-7" || issue.Resolved != nil || issue.Tags[0].Name != "Star" {
		t.Errorf("issue = %+v", issue)
	}

	values := map[string][]FieldValue{}
	for _, f := range issue.CustomFields {
		v, err := f.Values()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		values[f.Name] = v
	}
	if v := values["State"]; len(v) != 1 || v[0].Name != "Open" || v[0].ID != "67-2" {
		t.Errorf("State = %+v", v)
	}
	if v := values["Assignee"]; len(v) != 1 || v[0].Login != "jane.doe" || v[0].FullName != "Jane Doe" {
		t.Errorf("Assignee = %+v", v)
	}
	if v := values["Fix versions"]; len(v) != 2 || v[1].Name != "1.1" {
		t.Errorf("Fix versions = %+v", v)
	}
	if v := values["Estimation"]; len(v) != 1 || v[0].Minutes != 90 || v[0].Presentation != "1h 30m" {
		t.Errorf("Estimation = %+v", v)
	}
	if v := values["Notes"]; len(v) != 1 || v[0].Text != "hello" {
		t.Errorf("Notes = %+v", v)
	}
	if v := values["Due Date"]; v != nil {
		t.Errorf("Due Date (primitive) = %+v, want nil", v)
	}
	if got := string(issue.CustomFields[5].Value); got != "1700000000000" {
		t.Errorf("Due Date raw = %s", got)
	}
	if v := values["Subsystem"]; v != nil {
		t.Errorf("Subsystem (null) = %+v, want nil", v)
	}
}

func TestProjectCustomFieldsDecodeBundles(t *testing.T) {
	client, _ := fakeYouTrack(t, ok(`[
		{"id":"92-1","$type":"StateProjectCustomField","canBeEmpty":false,
		 "field":{"id":"58-2","name":"State","fieldType":{"id":"state[1]","valueType":"state","isMultiValue":false}},
		 "bundle":{"id":"40-1","values":[{"id":"67-1","name":"Open","isResolved":false},{"id":"67-9","name":"Done","isResolved":true}]}},
		{"id":"94-0","$type":"UserProjectCustomField","canBeEmpty":true,"emptyFieldText":"Unassigned",
		 "field":{"id":"58-4","name":"Assignee","fieldType":{"id":"user[1]","valueType":"user","isMultiValue":false}},
		 "bundle":{"id":"41-1","aggregatedUsers":[{"id":"1-2","login":"jane.doe","fullName":"Jane Doe"}]}},
		{"id":"96-0","$type":"PeriodProjectCustomField","canBeEmpty":true,
		 "field":{"id":"58-7","name":"Estimation","fieldType":{"id":"period","valueType":"period","isMultiValue":false}}}
	]`))
	fields, err := client.ProjectCustomFields(context.Background(), "0-0", Page{})
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 {
		t.Fatalf("fields = %d", len(fields))
	}
	state := fields[0]
	if state.Field.FieldType.ID != "state[1]" || len(state.Bundle.Values) != 2 || !state.Bundle.Values[1].IsResolved {
		t.Errorf("state = %+v", state)
	}
	assignee := fields[1]
	if !assignee.CanBeEmpty || assignee.EmptyFieldText != "Unassigned" || assignee.Bundle.Users[0].Login != "jane.doe" {
		t.Errorf("assignee = %+v", assignee)
	}
	if fields[2].Bundle != nil || fields[2].Type != "PeriodProjectCustomField" {
		t.Errorf("period = %+v", fields[2])
	}
}

func TestUploadAttachment(t *testing.T) {
	client, seen := fakeYouTrack(t, ok(`[{"id":"8-1","name":"notes.txt","size":5}]`))
	att, err := client.UploadAttachment(context.Background(), "ABC-1", "notes.txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if att.ID != "8-1" || att.Name != "notes.txt" {
		t.Errorf("attachment = %+v", att)
	}
	req := (*seen)[0]
	if req.Method != "POST" || req.Path != "/api/issues/ABC-1/attachments" {
		t.Errorf("request = %s %s", req.Method, req.Path)
	}
	if !strings.HasPrefix(req.ContentType, "multipart/form-data; boundary=") {
		t.Errorf("Content-Type = %q", req.ContentType)
	}
	if req.FileName != "notes.txt" || req.FileContent != "hello" {
		t.Errorf("file = %q %q", req.FileName, req.FileContent)
	}
}

func TestUploadAttachmentWithoutCreatedEntity(t *testing.T) {
	client, _ := fakeYouTrack(t, ok(`[]`))
	if _, err := client.UploadAttachment(context.Background(), "ABC-1", "a.txt", strings.NewReader("x")); err == nil {
		t.Error("want error when the server returns no attachment")
	}
}

const boardJSON = `{"id":"108-4","name":"Team Board",
	"columnSettings":{
		"field":{"id":"58-2","name":"State","fieldType":{"id":"state[1]","valueType":"state","isMultiValue":false}},
		"columns":[
			{"id":"c1","presentation":"Open","ordinal":0,"fieldValues":[{"id":"v1","name":"Open"}]},
			{"id":"c2","presentation":"In Progress, Review","ordinal":1,"fieldValues":[{"id":"v2","name":"In Progress"},{"id":"v3","name":"Review"}]},
			{"id":"c3","presentation":"Done","isResolved":true,"ordinal":2,"fieldValues":[{"id":"v4","name":"Done","isResolved":true}]}
		]}}`

func stateIssue(id, state string) string {
	value := "null"
	if state != "" {
		value = `{"name":"` + state + `"}`
	}
	return `{"idReadable":"` + id + `","customFields":[{"name":"State","$type":"StateIssueCustomField","value":` + value + `}]}`
}

func TestSprintBoardGroupsIssuesByColumn(t *testing.T) {
	sprint := `{"id":"109-8","name":"Sprint 5","issues":[` + strings.Join([]string{
		stateIssue("A-1", "Open"), stateIssue("A-2", "Review"), stateIssue("A-3", "In Progress"),
		stateIssue("A-4", "Blocked"), stateIssue("A-5", ""),
	}, ",") + `]}`
	client, seen := fakeYouTrack(t, ok(boardJSON), ok(sprint))

	board, err := client.SprintBoard(context.Background(), "108-4", "109-8")
	if err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[1].Path; got != "/api/agiles/108-4/sprints/109-8" {
		t.Errorf("sprint path = %q", got)
	}
	if board.Sprint.Name != "Sprint 5" || board.Agile.Name != "Team Board" {
		t.Errorf("board = %+v", board)
	}
	ids := func(issues []Issue) []string {
		var out []string
		for _, issue := range issues {
			out = append(out, issue.IDReadable)
		}
		return out
	}
	want := [][]string{{"A-1"}, {"A-2", "A-3"}, nil}
	for i, column := range board.Columns {
		if got := ids(column.Issues); !reflect.DeepEqual(got, want[i]) {
			t.Errorf("column %q = %v, want %v", column.Column.Presentation, got, want[i])
		}
	}
	if got := ids(board.Unmatched); !reflect.DeepEqual(got, []string{"A-4", "A-5"}) {
		t.Errorf("unmatched = %v", got)
	}
}

func TestSprintBoardStopsOnBoardError(t *testing.T) {
	client, seen := fakeYouTrack(t, reply{404, `{"error":"Not Found","error_description":"Entity with id 108-9 not found"}`})
	_, err := client.SprintBoard(context.Background(), "108-9", currentSprintID)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Errorf("error = %v", err)
	}
	if len(*seen) != 1 {
		t.Errorf("requests = %d, want 1", len(*seen))
	}
}

func TestMoveCardToColumn(t *testing.T) {
	var board Agile
	if err := json.Unmarshal([]byte(boardJSON), &board); err != nil {
		t.Fatal(err)
	}
	client, seen := fakeYouTrack(t, ok(`{"idReadable":"A-1"}`))
	if _, err := client.MoveCardToColumn(context.Background(), "A-1", board, board.ColumnSettings.Columns[1]); err != nil {
		t.Fatal(err)
	}
	req := (*seen)[0]
	if req.Method != "POST" || req.Path != "/api/issues/A-1" {
		t.Errorf("request = %s %s", req.Method, req.Path)
	}
	assertJSON(t, req.Body, `{"customFields":[{"name":"State","$type":"StateIssueCustomField","value":{"name":"In Progress"}}]}`)
}

func TestMoveCardToColumnRejectsUnusableBoard(t *testing.T) {
	var board Agile
	if err := json.Unmarshal([]byte(boardJSON), &board); err != nil {
		t.Fatal(err)
	}
	client, seen := fakeYouTrack(t)
	ctx := context.Background()

	if _, err := client.MoveCardToColumn(ctx, "A-1", board, AgileColumn{Presentation: "Empty"}); err == nil {
		t.Error("column without values: want error")
	}
	noField := board
	noField.ColumnSettings.Field = nil
	if _, err := client.MoveCardToColumn(ctx, "A-1", noField, board.ColumnSettings.Columns[0]); err == nil {
		t.Error("board without column field: want error")
	}
	if len(*seen) != 0 {
		t.Errorf("requests = %d, want 0", len(*seen))
	}
}

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{BaseURL: "https://example.youtrack.cloud", Token: testToken}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("config = %+v, want %+v", got, want)
	}
	info, err := os.Stat(filepath.Join(dir, configFileName))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
}

func TestSaveConfigTightensExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, configFileName)
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(dir, Config{Token: testToken}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadConfig(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: error = %v, want os.ErrNotExist", err)
	}
	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(dir); err == nil {
		t.Error("corrupt file: want error")
	}
}

func TestInsecureClientAcceptsAnUntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"login":"ana"}`)
	}))
	defer srv.Close()
	ctx := context.Background()

	if _, err := New(srv.URL, testToken).Me(ctx); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("New accepted an untrusted certificate: %v", err)
	}
	me, err := NewInsecure(srv.URL, testToken).Me(ctx)
	if err != nil || me.Login != "ana" {
		t.Fatalf("NewInsecure answered %+v, %v; want the user", me, err)
	}
	for insecure, wantErr := range map[bool]bool{false: true, true: false} {
		_, err := Config{BaseURL: srv.URL, Token: testToken, Insecure: insecure}.Client().Me(ctx)
		if (err != nil) != wantErr {
			t.Errorf("Config{Insecure: %v}.Client(): %v", insecure, err)
		}
	}
}

func TestAvatar(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 32)
	var authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/hub/avatar/ana":
			_, _ = io.WriteString(w, png)
		case "/hub/avatar/default":
			_, _ = io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
		case "/hub/avatar/huge":
			_, _ = io.WriteString(w, png+strings.Repeat("x", maxAvatarBytes))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	// The instance lives under a path; the picture's path starts at the root.
	c := New(srv.URL+"/youtrack", testToken)

	data, err := c.Avatar(ctx, "/hub/avatar/ana?s=48")
	if err != nil || string(data) != png {
		t.Fatalf("Avatar = %q, %v; want the picture", data, err)
	}
	if authorization != "Bearer "+testToken {
		t.Errorf("the instance got Authorization %q; want the token", authorization)
	}
	if _, err := c.Avatar(ctx, "/hub/avatar/default"); !errors.Is(err, ErrAvatarFormat) {
		t.Errorf("an SVG answered %v; want ErrAvatarFormat", err)
	}
	if _, err := c.Avatar(ctx, "/hub/avatar/huge"); err == nil {
		t.Error("a picture over the limit was accepted")
	}
	var apiErr *APIError
	if _, err := c.Avatar(ctx, "/hub/avatar/nobody"); !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Errorf("a missing picture answered %v; want a 404 APIError", err)
	}

	elsewhere := New("http://youtrack.invalid", testToken)
	if _, err := elsewhere.Avatar(ctx, srv.URL+"/hub/avatar/ana"); err != nil {
		t.Fatalf("an absolute url answered %v", err)
	}
	if authorization != "" {
		t.Errorf("another host got Authorization %q; want none", authorization)
	}
}
