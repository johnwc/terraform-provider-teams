package provider

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
)

const (
	testAAAppID = "ce933385-9390-45d1-9512-c8d228074e07"
	testCQAppID = "11cd3e2e-fccb-42ad-ad00-878b93575e07"
)

func TestResourceAccountAppID(t *testing.T) {
	for typ, want := range map[string]string{"auto_attendant": testAAAppID, "call_queue": testCQAppID} {
		got, err := resourceAccountAppID(typ)
		if err != nil || got != want {
			t.Errorf("resourceAccountAppID(%q) = %q, %v; want %q", typ, got, err, want)
		}
		back, err := resourceAccountType(want)
		if err != nil || back != typ {
			t.Errorf("resourceAccountType(%q) = %q, %v; want %q", want, back, err, typ)
		}
	}
	if got, err := resourceAccountType("11CD3E2E-FCCB-42AD-AD00-878B93575E07"); err != nil || got != "call_queue" {
		t.Errorf("upper-case id = %q, %v", got, err)
	}
	if _, err := resourceAccountAppID("bogus"); err == nil {
		t.Error("resourceAccountAppID(bogus) should fail")
	}
	if _, err := resourceAccountType("00000000-0000-0000-0000-000000000000"); err == nil {
		t.Error("resourceAccountType(unknown id) should fail")
	}
}

func TestResourceAccountBody(t *testing.T) {
	m := resourceAccountModel{
		UserPrincipalName: types.StringValue("ra-main@contoso.com"),
		DisplayName:       types.StringValue("Main line"),
		Type:              types.StringValue("call_queue"),
	}
	body, diags := resourceAccountBody(&m)
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	want := map[string]any{"userPrincipalName": "ra-main@contoso.com", "applicationId": testCQAppID, "displayName": "Main line"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v\nwant %#v", body, want)
	}
	m.Type = types.StringValue("bogus")
	if _, diags := resourceAccountBody(&m); !diags.HasError() {
		t.Error("expected an error for an unknown type")
	}
}

func TestResourceAccountModelFromObject(t *testing.T) {
	m := resourceAccountModel{
		ID:                types.StringUnknown(),
		UserPrincipalName: types.StringValue("RA-Main@contoso.com"),
		DisplayName:       types.StringValue("Main line"),
		Type:              types.StringValue("auto_attendant"),
		PhoneNumber:       types.StringUnknown(),
	}
	obj := map[string]any{
		"applicationId":     testAAAppID,
		"displayName":       "Main line",
		"objectId":          "obj-1",
		"userPrincipalName": "ra-main@contoso.com",
		"tenantId":          "tid",
	}
	if diags := resourceAccountModelFromObject(obj, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if m.ID.ValueString() != "obj-1" {
		t.Errorf("id = %s", m.ID)
	}
	if m.UserPrincipalName.ValueString() != "RA-Main@contoso.com" {
		t.Errorf("user_principal_name = %s; want the prior casing kept", m.UserPrincipalName)
	}
	if m.Type.ValueString() != "auto_attendant" {
		t.Errorf("type = %s", m.Type)
	}
	if !m.PhoneNumber.IsNull() {
		t.Errorf("phone_number = %s; want null", m.PhoneNumber)
	}

	obj["phoneNumber"] = "tel:+18005550100"
	if diags := resourceAccountModelFromObject(obj, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if m.PhoneNumber.ValueString() != "+18005550100" {
		t.Errorf("phone_number = %s", m.PhoneNumber)
	}

	obj["applicationId"] = "00000000-0000-0000-0000-000000000000"
	diags := resourceAccountModelFromObject(obj, &m)
	if !diags.HasError() {
		t.Fatal("expected an error for an unknown applicationId")
	}
	if got := diags.Errors()[0].Summary(); got != "Unexpected applicationId value" {
		t.Errorf("summary = %q", got)
	}
}

func TestResourceAccountModelFromObjectImport(t *testing.T) {
	m := resourceAccountModel{
		ID:                types.StringValue("obj-1"),
		UserPrincipalName: types.StringNull(),
		DisplayName:       types.StringNull(),
		Type:              types.StringNull(),
		PhoneNumber:       types.StringNull(),
	}
	obj := map[string]any{"applicationId": testCQAppID, "displayName": "Queue", "objectId": "obj-1", "userPrincipalName": "ra-q@contoso.com"}
	if diags := resourceAccountModelFromObject(obj, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if m.UserPrincipalName.ValueString() != "ra-q@contoso.com" || m.DisplayName.ValueString() != "Queue" || m.Type.ValueString() != "call_queue" {
		t.Errorf("model = %+v", m)
	}
}

func TestResourceAccountPhoneValue(t *testing.T) {
	cases := map[any]types.String{
		"tel:+18005550100": types.StringValue("+18005550100"),
		"TEL:+18005550100": types.StringValue("+18005550100"),
		"+18005550100":     types.StringValue("+18005550100"),
		"":                 types.StringNull(),
		nil:                types.StringNull(),
	}
	for in, want := range cases {
		if got := resourceAccountPhoneValue(in); !got.Equal(want) {
			t.Errorf("resourceAccountPhoneValue(%#v) = %s; want %s", in, got, want)
		}
	}
}

func TestIsResourceAccountGone(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404", &teamsapi.APIError{Status: 404, Message: "Not Found"}, true},
		{"400 not found", &teamsapi.APIError{Status: 400, Message: "Application instance not found."}, true},
		{"other 400", &teamsapi.APIError{Status: 400, Message: "ApplicationId is required"}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isResourceAccountGone(tc.err); got != tc.want {
				t.Errorf("isResourceAccountGone = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestResourceAccountOps(t *testing.T) {
	op := resourceAccountKind.op("New", "POST", resourceAccountKind.path)
	if op.CmdletName != "New-CsOnlineApplicationInstance" || op.Path != "/Teams.PlatformService/v2/ApplicationInstances" ||
		op.Transport != teamsapi.ConfigAPI || op.Kind != teamsapi.AutoRest {
		t.Errorf("op = %#v", op)
	}
	obj := map[string]any{"objectId": "obj-1"}
	if got := resourceAccountKind.unwrap([]map[string]any{obj}); !reflect.DeepEqual(got, obj) {
		t.Errorf("unwrap = %#v", got)
	}
}
