package youtrack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// $type values from devportal: api-concept-type ("$type for Custom Fields").
const (
	TypeSingleEnum    = "SingleEnumIssueCustomField"
	TypeMultiEnum     = "MultiEnumIssueCustomField"
	TypeSingleBuild   = "SingleBuildIssueCustomField"
	TypeMultiBuild    = "MultiBuildIssueCustomField"
	TypeState         = "StateIssueCustomField"
	TypeSingleVersion = "SingleVersionIssueCustomField"
	TypeMultiVersion  = "MultiVersionIssueCustomField"
	TypeSingleOwned   = "SingleOwnedIssueCustomField"
	TypeMultiOwned    = "MultiOwnedIssueCustomField"
	TypeSingleUser    = "SingleUserIssueCustomField"
	TypeMultiUser     = "MultiUserIssueCustomField"
	TypeSingleGroup   = "SingleGroupIssueCustomField"
	TypeMultiGroup    = "MultiGroupIssueCustomField"
	TypeSimple        = "SimpleIssueCustomField"
	TypeDate          = "DateIssueCustomField"
	TypePeriod        = "PeriodIssueCustomField"
	TypeText          = "TextIssueCustomField"
)

var issueFieldTypes = map[string]string{
	"enum[1]":       TypeSingleEnum,
	"enum[*]":       TypeMultiEnum,
	"build[1]":      TypeSingleBuild,
	"build[*]":      TypeMultiBuild,
	"state[1]":      TypeState,
	"version[1]":    TypeSingleVersion,
	"version[*]":    TypeMultiVersion,
	"ownedField[1]": TypeSingleOwned,
	"ownedField[*]": TypeMultiOwned,
	"user[1]":       TypeSingleUser,
	"user[*]":       TypeMultiUser,
	"group[1]":      TypeSingleGroup,
	"group[*]":      TypeMultiGroup,
	// The docs table names this type "int"; instances report "integer".
	"int":           TypeSimple,
	"integer":       TypeSimple,
	"float":         TypeSimple,
	"string":        TypeSimple,
	"date and time": TypeSimple,
	"date":          TypeDate,
	"period":        TypePeriod,
	"text":          TypeText,
}

// IssueFieldType maps a FieldType id (ProjectCustomField.Field.FieldType.ID)
// to the $type an issue update must carry.
func IssueFieldType(fieldTypeID string) (string, error) {
	t, ok := issueFieldTypes[fieldTypeID]
	if !ok {
		return "", fmt.Errorf("youtrack: unknown custom field type %q", fieldTypeID)
	}
	return t, nil
}

type CustomFieldUpdate struct {
	Name  string `json:"name"`
	Type  string `json:"$type"`
	Value any    `json:"value"`
}

type namedValue struct {
	Name string `json:"name"`
}

type loginValue struct {
	Login string `json:"login"`
}

func namedValues(names []string) []namedValue {
	values := make([]namedValue, len(names))
	for i, n := range names {
		values[i] = namedValue{Name: n}
	}
	return values
}

func EnumField(field, value string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSingleEnum, namedValue{value}}
}

func MultiEnumField(field string, values ...string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeMultiEnum, namedValues(values)}
}

func StateField(field, value string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeState, namedValue{value}}
}

func VersionField(field, value string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSingleVersion, namedValue{value}}
}

func MultiVersionField(field string, values ...string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeMultiVersion, namedValues(values)}
}

func BuildField(field, value string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSingleBuild, namedValue{value}}
}

func MultiBuildField(field string, values ...string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeMultiBuild, namedValues(values)}
}

func OwnedField(field, value string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSingleOwned, namedValue{value}}
}

func MultiOwnedField(field string, values ...string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeMultiOwned, namedValues(values)}
}

func GroupField(field, group string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSingleGroup, namedValue{group}}
}

func MultiGroupField(field string, groups ...string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeMultiGroup, namedValues(groups)}
}

func UserField(field, login string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSingleUser, loginValue{login}}
}

func MultiUserField(field string, logins ...string) CustomFieldUpdate {
	values := make([]loginValue, len(logins))
	for i, l := range logins {
		values[i] = loginValue{Login: l}
	}
	return CustomFieldUpdate{field, TypeMultiUser, values}
}

func PeriodField(field string, minutes int) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypePeriod, struct {
		Minutes int `json:"minutes"`
	}{minutes}}
}

func DateField(field string, date time.Time) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeDate, millis(date)}
}

func DateTimeField(field string, at time.Time) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSimple, millis(at)}
}

func TextField(field, text string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeText, struct {
		Text string `json:"text"`
	}{text}}
}

func StringField(field, value string) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSimple, value}
}

func IntField(field string, value int) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSimple, value}
}

func FloatField(field string, value float64) CustomFieldUpdate {
	return CustomFieldUpdate{field, TypeSimple, value}
}

// ClearField needs the field's own $type (see IssueFieldType); YouTrack
// clears a field when value is null.
func ClearField(field, issueFieldType string) CustomFieldUpdate {
	return CustomFieldUpdate{field, issueFieldType, nil}
}

type IssueCustomField struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Type  string          `json:"$type"`
	Value json.RawMessage `json:"value"`
}

// FieldValue covers every entity a custom field can hold (bundle element,
// user, group, period, text); only the attributes of the actual kind are set.
type FieldValue struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Login        string `json:"login"`
	FullName     string `json:"fullName"`
	AvatarURL    string `json:"avatarUrl"`
	Minutes      int    `json:"minutes"`
	Presentation string `json:"presentation"`
	Text         string `json:"text"`
	IsResolved   bool   `json:"isResolved"`
}

const customFieldValueFields = "id,name,login,fullName,avatarUrl,minutes,presentation,text,isResolved"

// Values decodes entity values, single or multiple. Primitive fields
// (string, number, date) hold no entity: read Value directly.
func (f IssueCustomField) Values() ([]FieldValue, error) {
	raw := bytes.TrimSpace(f.Value)
	if len(raw) == 0 {
		return nil, nil
	}
	switch raw[0] {
	case '{':
		var v FieldValue
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("youtrack: field %q: %w", f.Name, err)
		}
		return []FieldValue{v}, nil
	case '[':
		var vs []FieldValue
		if err := json.Unmarshal(raw, &vs); err != nil {
			return nil, fmt.Errorf("youtrack: field %q: %w", f.Name, err)
		}
		return vs, nil
	}
	return nil, nil
}
