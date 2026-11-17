package provider

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
)

func testAssociationResult(errorCode any, message string) []map[string]any {
	return []map[string]any{{
		"AssociationOperationResults": []any{map[string]any{
			"ApplicationInstanceAssociation": map[string]any{
				"ApplicationConfigurationType": "CallQueue",
				"CallPriority":                 float64(2),
				"ConfigurationId":              "cq-1",
				"Id":                           "ra-1",
			},
			"StatusRecord": map[string]any{"ErrorCode": errorCode, "Message": message},
		}},
	}}
}

func TestAssociationBody(t *testing.T) {
	m := resourceAccountAssociationModel{
		ResourceAccountID: types.StringValue("ra-1"),
		ConfigurationID:   types.StringValue("aa-1"),
		ConfigurationType: types.StringValue("auto_attendant"),
		CallPriority:      types.Int64Value(3),
	}
	body, diags := associationBody(&m)
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	want := map[string]any{"EndpointsIds": []string{"ra-1"}, "ConfigurationId": "aa-1", "ConfigurationType": "AutoAttendant", "CallPriority": int64(3)}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v\nwant %#v", body, want)
	}
	m.ConfigurationType = types.StringValue("bogus")
	if _, diags := associationBody(&m); !diags.HasError() {
		t.Error("expected an error for an unknown configuration_type")
	}
}

func TestParseAssociationCreate(t *testing.T) {
	assoc, err := parseAssociationCreate(testAssociationResult(nil, "Success"))
	if err != nil {
		t.Fatalf("parseAssociationCreate: %v", err)
	}
	if getString(assoc, "Id") != "ra-1" {
		t.Errorf("association = %#v", assoc)
	}

	_, err = parseAssociationCreate(testAssociationResult("AddApplicationInstanceAssociation_InvalidConfiguration", "The configuration does not exist."))
	if err == nil || !strings.Contains(err.Error(), "The configuration does not exist.") {
		t.Errorf("err = %v; want the StatusRecord message", err)
	}

	if _, err := parseAssociationCreate([]map[string]any{{}}); err == nil {
		t.Error("expected an error for a response without results")
	}
	if _, err := parseAssociationCreate(nil); err == nil {
		t.Error("expected an error for an empty response")
	}
}

func TestIsEndpointProvisioning(t *testing.T) {
	_, err := parseAssociationCreate(testAssociationResult(associationEndpointNotFoundCode, "Endpoint missing."))
	if !isEndpointProvisioning(err) {
		t.Errorf("isEndpointProvisioning(%v) = false", err)
	}
	_, err = parseAssociationCreate(testAssociationResult("AddApplicationInstanceAssociation_InvalidConfiguration", "AppEndpointNotFound"))
	if isEndpointProvisioning(err) {
		t.Errorf("isEndpointProvisioning(%v) = true; want false for another code", err)
	}
	if isEndpointProvisioning(nil) || isEndpointProvisioning(errors.New("AppEndpointNotFound")) {
		t.Error("untyped or nil errors reported as provisioning")
	}
}

func TestParseAssociationDelete(t *testing.T) {
	cases := []struct {
		name    string
		v       []map[string]any
		wantErr bool
	}{
		{"success", testAssociationResult(nil, "Success"), false},
		{"not found", testAssociationResult(associationNotFoundCode, "Association not found."), false},
		{"other error", testAssociationResult("RemoveApplicationInstanceAssociation_Failed", "Boom."), true},
		{"empty body", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := parseAssociationDelete(tc.v); (err != nil) != tc.wantErr {
				t.Errorf("err = %v; wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAssociationModelFromObject(t *testing.T) {
	v := []map[string]any{{
		"ApplicationInstanceAssociation": map[string]any{
			"ApplicationConfigurationType": "CallQueue",
			"CallPriority":                 float64(3),
			"ConfigurationId":              "CQ-1",
			"Id":                           "ra-1",
		},
		"Diagnostics": []any{},
	}}
	assoc := unwrapAssociation(v)
	if assoc == nil {
		t.Fatal("unwrapAssociation returned nil")
	}
	m := resourceAccountAssociationModel{
		ID:                types.StringValue("ra-1"),
		ResourceAccountID: types.StringNull(),
		ConfigurationID:   types.StringValue("cq-1"),
		ConfigurationType: types.StringNull(),
		CallPriority:      types.Int64Null(),
	}
	if diags := associationModelFromObject(assoc, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if m.ResourceAccountID.ValueString() != "ra-1" || m.ConfigurationID.ValueString() != "cq-1" ||
		m.ConfigurationType.ValueString() != "call_queue" || m.CallPriority.ValueInt64() != 3 {
		t.Errorf("model = %+v", m)
	}

	assoc["ApplicationConfigurationType"] = "Bogus"
	if diags := associationModelFromObject(assoc, &m); !diags.HasError() {
		t.Error("expected an error for an unknown ApplicationConfigurationType")
	}
	if got := unwrapAssociation(nil); got != nil {
		t.Errorf("unwrapAssociation(nil) = %#v", got)
	}
}

func TestIsAssociationGone(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404", &teamsapi.APIError{Status: 404, Message: "Not Found"}, true},
		{"400 association not found", &teamsapi.APIError{Status: 400, Message: "Association not found"}, true},
		{"other 400", &teamsapi.APIError{Status: 400, Message: "Invalid priority"}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAssociationGone(tc.err); got != tc.want {
				t.Errorf("isAssociationGone = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestAssociationOps(t *testing.T) {
	op := associationKind.op("Remove", "DELETE", associationKind.itemPath())
	if op.CmdletName != "Remove-CsOnlineApplicationInstanceAssociation" || op.Path != "/Teams.VoiceApps/applicationinstanceassociations/{identity}" {
		t.Errorf("op = %#v", op)
	}
}
