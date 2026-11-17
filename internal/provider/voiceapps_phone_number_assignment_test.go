package provider

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
)

// testResolvePath substitutes {identity} the way the teamsapi AutoRest transport does.
func testResolvePath(t *testing.T, p, identity string) *url.URL {
	t.Helper()
	u, err := url.Parse("https://api.example" + strings.ReplaceAll(p, "{identity}", url.PathEscape(identity)))
	if err != nil {
		t.Fatalf("parse %q: %v", p, err)
	}
	return u
}

func TestPhoneAssignPath(t *testing.T) {
	p := phoneAssignPath("+18005550100", "CallingPlan", "")
	want := "/Skype.TelephoneNumberMgmt/cmdlet/assignment?identity={identity}&phoneNumber=%2B18005550100&phoneNumberType=CallingPlan&assignmentCategory=Primary"
	if p != want {
		t.Errorf("path = %q\nwant %q", p, want)
	}
	u := testResolvePath(t, p, "ra-1")
	q := u.Query()
	if u.Path != "/Skype.TelephoneNumberMgmt/cmdlet/assignment" || q.Get("identity") != "ra-1" || q.Get("phoneNumber") != "+18005550100" ||
		q.Get("phoneNumberType") != "CallingPlan" || q.Get("assignmentCategory") != "Primary" || q.Has("locationId") {
		t.Errorf("resolved = %s", u)
	}

	u = testResolvePath(t, phoneAssignPath("+18005550100", "DirectRouting", "loc 1&x"), "ra-1")
	if got := u.Query().Get("locationId"); got != "loc 1&x" {
		t.Errorf("locationId = %q", got)
	}
}

func TestPhoneAssignOpBody(t *testing.T) {
	op := phoneNumberAssignmentKind.op("Set", "POST", phoneAssignPath("+18005550100", "CallingPlan", ""))
	if op.CmdletName != "Set-CsPhoneNumberAssignment" || op.Kind != teamsapi.AutoRest || op.Transport != teamsapi.ConfigAPI {
		t.Errorf("op = %#v", op)
	}
	if !strings.Contains(op.Path, "{identity}") {
		t.Error("the path must carry the {identity} placeholder so the Identity parameter is left out of the body")
	}
}

func TestPhoneUnassignPath(t *testing.T) {
	p := phoneUnassignPath("+18005550100", "OperatorConnect")
	if want := "/Skype.TelephoneNumberMgmt/cmdlet/unassignment/user/{identity}?phoneNumber=%2B18005550100&phoneNumberType=OperatorConnect"; p != want {
		t.Errorf("path = %q\nwant %q", p, want)
	}
	u := testResolvePath(t, p, "ra-1")
	if u.Path != "/Skype.TelephoneNumberMgmt/cmdlet/unassignment/user/ra-1" || u.Query().Get("phoneNumber") != "+18005550100" {
		t.Errorf("resolved = %s", u)
	}
	if got := phoneNumberAssignmentKind.cmdlet("Remove"); got != "Remove-CsPhoneNumberAssignment" {
		t.Errorf("cmdlet = %q", got)
	}
}

func TestPhoneAssignmentsPath(t *testing.T) {
	if got, want := phoneAssignmentsPath("+18005550100"), "/Skype.TelephoneNumberMgmt/cmdlet/assignments?telephoneNumber=%2B18005550100"; got != want {
		t.Errorf("path = %q; want %q", got, want)
	}
}

func testPhoneAssignment(number, target string) map[string]any {
	var t any
	if target != "" {
		t = target
	}
	return map[string]any{
		"TelephoneNumber":      number,
		"NumberType":           "CallingPlan",
		"AssignedPstnTargetId": t,
		"AssignmentCategory":   "Primary",
		"PstnAssignmentStatus": "VoiceApplicationAssigned",
		"Capabilities":         []any{"VoiceApplicationAssignment"},
		"LocationId":           "loc-1",
	}
}

func TestMatchPhoneAssignment(t *testing.T) {
	list := []map[string]any{
		testPhoneAssignment("+18005550199", "ra-1"),
		testPhoneAssignment("+18005550100", "RA-1"),
	}
	cases := []struct {
		name     string
		v        []map[string]any
		number   string
		identity string
		want     bool
	}{
		{"match ignoring case", list, "+18005550100", "ra-1", true},
		{"other target", list, "+18005550100", "ra-2", false},
		{"number missing", list, "+18005550111", "ra-1", false},
		{"unassigned", []map[string]any{testPhoneAssignment("+18005550100", "")}, "+18005550100", "ra-1", false},
		{"tel prefix", []map[string]any{testPhoneAssignment("tel:+18005550100", "ra-1")}, "+18005550100", "ra-1", true},
		{"value wrapper", []map[string]any{{"value": []any{testPhoneAssignment("+18005550100", "ra-1")}}}, "+18005550100", "ra-1", true},
		{"empty", nil, "+18005550100", "ra-1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchPhoneAssignment(tc.v, tc.number, tc.identity); (got != nil) != tc.want {
				t.Errorf("match = %#v; want found %v", got, tc.want)
			}
		})
	}
}

func TestPhoneAssignmentModelFromObject(t *testing.T) {
	m := phoneNumberAssignmentModel{
		ID:              types.StringValue("ra-1/+18005550100"),
		Identity:        types.StringValue("ra-1"),
		PhoneNumber:     types.StringValue("+18005550100"),
		PhoneNumberType: types.StringNull(),
		LocationID:      types.StringNull(),
	}
	if diags := phoneAssignmentModelFromObject(testPhoneAssignment("+18005550100", "ra-1"), &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if m.PhoneNumberType.ValueString() != "calling_plan" || m.LocationID.ValueString() != "loc-1" || m.ID.ValueString() != "ra-1/+18005550100" {
		t.Errorf("model = %+v", m)
	}
	obj := testPhoneAssignment("+18005550100", "ra-1")
	obj["NumberType"] = "Bogus"
	if diags := phoneAssignmentModelFromObject(obj, &m); !diags.HasError() {
		t.Error("expected an error for an unknown NumberType")
	}
}

func TestParsePhoneAssignmentID(t *testing.T) {
	identity, number, err := parsePhoneAssignmentID("11111111-2222-3333-4444-555555555555/+18005550100")
	if err != nil || identity != "11111111-2222-3333-4444-555555555555" || number != "+18005550100" {
		t.Errorf("parse = %q, %q, %v", identity, number, err)
	}
	for _, bad := range []string{"", "ra-1", "/+18005550100", "ra-1/", "ra-1/18005550100", "ra-1/+1800-555-0100"} {
		if _, _, err := parsePhoneAssignmentID(bad); err == nil {
			t.Errorf("parsePhoneAssignmentID(%q) should fail", bad)
		}
	}
	if got := phoneAssignmentID("ra-1", "+18005550100"); got != "ra-1/+18005550100" {
		t.Errorf("phoneAssignmentID = %q", got)
	}
}

func TestPhoneNumberTypes(t *testing.T) {
	for tf, wire := range map[string]string{"calling_plan": "CallingPlan", "direct_routing": "DirectRouting", "operator_connect": "OperatorConnect"} {
		if got, ok := phoneNumberTypes.toWire(tf); !ok || got != wire {
			t.Errorf("toWire(%q) = %q, %v", tf, got, ok)
		}
		if got, ok := phoneNumberTypes.fromWireWord(wire); !ok || got != tf {
			t.Errorf("fromWireWord(%q) = %q, %v", wire, got, ok)
		}
	}
}

func TestE164Validator(t *testing.T) {
	for in, ok := range map[string]bool{"+18005550100": true, "+442071838750": true, "18005550100": false, "+0800": false, "+1 800 555 0100": false, "tel:+18005550100": false} {
		resp := &validator.StringResponse{}
		e164Validator{}.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("phone_number"), ConfigValue: types.StringValue(in)}, resp)
		if resp.Diagnostics.HasError() == ok {
			t.Errorf("%q: HasError = %v; want valid %v", in, resp.Diagnostics.HasError(), ok)
		}
	}
}
