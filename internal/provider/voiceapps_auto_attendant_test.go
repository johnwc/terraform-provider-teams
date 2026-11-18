package provider

import (
	"context"
	"reflect"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
)

func TestAutoAttendantEnums(t *testing.T) {
	cases := []struct {
		name string
		enum wireEnum
		tf   string
		wire string
	}{
		{"voice female", aaVoices, "female", "Female"},
		{"voice male", aaVoices, "male", "Male"},
		{"key automatic", aaMenuKeys, "automatic", "Automatic"},
		{"key 0", aaMenuKeys, "0", "Tone0"},
		{"key 1", aaMenuKeys, "1", "Tone1"},
		{"key 2", aaMenuKeys, "2", "Tone2"},
		{"key 3", aaMenuKeys, "3", "Tone3"},
		{"key 4", aaMenuKeys, "4", "Tone4"},
		{"key 5", aaMenuKeys, "5", "Tone5"},
		{"key 6", aaMenuKeys, "6", "Tone6"},
		{"key 7", aaMenuKeys, "7", "Tone7"},
		{"key 8", aaMenuKeys, "8", "Tone8"},
		{"key 9", aaMenuKeys, "9", "Tone9"},
		{"key star", aaMenuKeys, "star", "ToneStar"},
		{"key pound", aaMenuKeys, "pound", "TonePound"},
		{"action disconnect", aaMenuActions, "disconnect", "DisconnectCall"},
		{"action transfer", aaMenuActions, "transfer", "TransferCallToTarget"},
		{"action transfer_to_operator", aaMenuActions, "transfer_to_operator", "TransferCallToOperator"},
		{"action announcement", aaMenuActions, "announcement", "Announcement"},
		{"target user", aaTargetTypes, "user", "User"},
		{"target application_endpoint", aaTargetTypes, "application_endpoint", "ApplicationEndpoint"},
		{"target external_pstn", aaTargetTypes, "external_pstn", "ExternalPstn"},
		{"target shared_voicemail", aaTargetTypes, "shared_voicemail", "SharedVoicemail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := tc.enum.toWire(tc.tf); !ok || got != tc.wire {
				t.Errorf("toWire(%q) = %q, %v; want %q", tc.tf, got, ok, tc.wire)
			}
			if got, ok := tc.enum.fromWireWord(tc.wire); !ok || got != tc.tf {
				t.Errorf("fromWireWord(%q) = %q, %v; want %q", tc.wire, got, ok, tc.tf)
			}
		})
	}
}

func TestAutoAttendantEnumsRejectUnknown(t *testing.T) {
	for _, e := range []wireEnum{aaVoices, aaMenuKeys, aaMenuActions, aaTargetTypes} {
		if _, ok := e.toWire("bogus"); ok {
			t.Error("toWire(bogus) should fail")
		}
		for _, in := range []any{float64(0), float64(1), 1, "Bogus", "Unknown", nil, true} {
			if got, ok := e.fromWireWord(in); ok {
				t.Errorf("fromWireWord(%#v) = %q; want failure", in, got)
			}
		}
	}
}

// testOption returns a menu option; empty strings are null.
func testOption(key, action, targetID, targetType, announcement string) aaMenuOptionModel {
	s := func(v string) types.String {
		if v == "" {
			return types.StringNull()
		}
		return types.StringValue(v)
	}
	return aaMenuOptionModel{
		Key: s(key), Action: s(action), TargetID: s(targetID), TargetType: s(targetType),
		Announcement: s(announcement), AnnouncementAudioFileID: types.StringNull(),
	}
}

// testAutoAttendantModel returns a model with the schema defaults applied and the given call flows.
func testAutoAttendantModel(t *testing.T, f aaFlows) autoAttendantModel {
	t.Helper()
	m := autoAttendantModel{
		ID:                   types.StringUnknown(),
		Name:                 types.StringValue("tf main"),
		LanguageID:           types.StringValue("en-US"),
		TimeZoneID:           types.StringValue("Central Standard Time"),
		VoiceID:              types.StringValue("female"),
		ApplicationInstances: types.ListUnknown(types.StringType),
	}
	if d := m.setFlows(context.Background(), f); d.HasError() {
		t.Fatalf("setFlows diagnostics: %v", d)
	}
	return m
}

// testFullFlows is a default flow with two transfers, after hours and two holidays.
// The second holiday has no id yet.
func testFullFlows() aaFlows {
	return aaFlows{
		Default: aaCallFlowModel{
			ID:         types.StringValue("flow-default"),
			Greeting:   types.StringValue("Welcome"),
			MenuPrompt: types.StringValue("Press 1 for sales, press 2 for support."),
			MenuOption: []aaMenuOptionModel{
				testOption("1", "transfer", "tel:+18005550101", "external_pstn", ""),
				testOption("2", "transfer", "tel:+18005550102", "external_pstn", ""),
			},
		},
		AfterHours: &aaScheduledCallFlowModel{
			ID:         types.StringValue("flow-ah"),
			ScheduleID: types.StringValue("sched-weekly"),
			Greeting:   types.StringValue("We are closed."),
			MenuPrompt: types.StringNull(),
			MenuOption: []aaMenuOptionModel{testOption("automatic", "disconnect", "", "", "")},
		},
		Holidays: []aaScheduledCallFlowModel{
			{
				ID:         types.StringValue("flow-h1"),
				ScheduleID: types.StringValue("sched-xmas"),
				Greeting:   types.StringNull(),
				MenuPrompt: types.StringNull(),
				MenuOption: []aaMenuOptionModel{testOption("automatic", "disconnect", "", "", "")},
			},
			{
				ID:         types.StringUnknown(),
				ScheduleID: types.StringValue("sched-ny"),
				Greeting:   types.StringNull(),
				MenuPrompt: types.StringValue("Press 1 for our holiday hours."),
				MenuOption: []aaMenuOptionModel{testOption("1", "announcement", "", "", "Happy new year")},
			},
		},
	}
}

func tts(s string) map[string]any {
	return map[string]any{"ActiveType": "TextToSpeech", "TextToSpeechPrompt": s}
}

var guidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestAutoAttendantBodyFull(t *testing.T) {
	ctx := context.Background()
	m := testAutoAttendantModel(t, testFullFlows())
	body, diags := autoAttendantBodyFromModel(ctx, &m)
	if diags.HasError() {
		t.Fatalf("autoAttendantBodyFromModel diagnostics: %v", diags)
	}

	flows, _ := body["CallFlows"].([]any)
	if len(flows) != 3 {
		t.Fatalf("CallFlows = %#v", body["CallFlows"])
	}
	genID, _ := flows[2].(map[string]any)["Id"].(string)
	if !guidV4.MatchString(genID) {
		t.Fatalf("generated call flow id %q is not a v4 GUID", genID)
	}

	want := map[string]any{
		"Name":       "tf main",
		"LanguageId": "en-US",
		"TimeZoneId": "Central Standard Time",
		"VoiceId":    "Female",
		"DefaultCallFlow": map[string]any{
			"Id":        "flow-default",
			"Name":      "tf main default",
			"Greetings": []any{tts("Welcome")},
			"Menu": map[string]any{
				"Name":    "tf main default menu",
				"Prompts": []any{tts("Press 1 for sales, press 2 for support.")},
				"MenuOptions": []any{
					map[string]any{"Action": "TransferCallToTarget", "DtmfResponse": "Tone1", "CallTarget": map[string]any{"Id": "tel:+18005550101", "Type": "ExternalPstn"}},
					map[string]any{"Action": "TransferCallToTarget", "DtmfResponse": "Tone2", "CallTarget": map[string]any{"Id": "tel:+18005550102", "Type": "ExternalPstn"}},
				},
			},
		},
		"CallFlows": []any{
			map[string]any{
				"Id":        "flow-ah",
				"Name":      "tf main after hours",
				"Greetings": []any{tts("We are closed.")},
				"Menu": map[string]any{
					"Name":        "tf main after hours menu",
					"Prompts":     []any{},
					"MenuOptions": []any{map[string]any{"Action": "DisconnectCall", "DtmfResponse": "Automatic"}},
				},
			},
			map[string]any{
				"Id":        "flow-h1",
				"Name":      "tf main holiday 1",
				"Greetings": []any{},
				"Menu": map[string]any{
					"Name":        "tf main holiday 1 menu",
					"Prompts":     []any{},
					"MenuOptions": []any{map[string]any{"Action": "DisconnectCall", "DtmfResponse": "Automatic"}},
				},
			},
			map[string]any{
				"Id":        genID,
				"Name":      "tf main holiday 2",
				"Greetings": []any{},
				"Menu": map[string]any{
					"Name":        "tf main holiday 2 menu",
					"Prompts":     []any{tts("Press 1 for our holiday hours.")},
					"MenuOptions": []any{map[string]any{"Action": "Announcement", "DtmfResponse": "Tone1", "Prompt": tts("Happy new year")}},
				},
			},
		},
		"CallHandlingAssociations": []any{
			map[string]any{"Type": "AfterHours", "ScheduleId": "sched-weekly", "CallFlowId": "flow-ah", "Enabled": true, "Priority": 1},
			map[string]any{"Type": "Holiday", "ScheduleId": "sched-xmas", "CallFlowId": "flow-h1", "Enabled": true, "Priority": 100},
			map[string]any{"Type": "Holiday", "ScheduleId": "sched-ny", "CallFlowId": genID, "Enabled": true, "Priority": 100},
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v\nwant %#v", body, want)
	}
	if _, ok := body["Schedules"]; ok {
		t.Error("body must not include Schedules")
	}

	f, d := m.flows(ctx)
	if d.HasError() {
		t.Fatalf("flows diagnostics: %v", d)
	}
	if f.Default.ID.ValueString() != "flow-default" || f.AfterHours.ID.ValueString() != "flow-ah" || f.Holidays[0].ID.ValueString() != "flow-h1" {
		t.Errorf("existing ids not reused: %s %s %s", f.Default.ID, f.AfterHours.ID, f.Holidays[0].ID)
	}
	if f.Holidays[1].ID.ValueString() != genID {
		t.Errorf("generated id not written back: %s; want %s", f.Holidays[1].ID, genID)
	}
}

func TestAutoAttendantBodyHolidaysWithoutAfterHours(t *testing.T) {
	f := testFullFlows()
	f.AfterHours = nil
	m := testAutoAttendantModel(t, f)
	body, diags := autoAttendantBodyFromModel(context.Background(), &m)
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	assocs := body["CallHandlingAssociations"].([]any)
	if len(assocs) != 2 {
		t.Fatalf("associations = %#v", assocs)
	}
	for i, a := range assocs {
		am := a.(map[string]any)
		if am["Type"] != "Holiday" || am["Priority"] != aaHolidayPriority {
			t.Errorf("association %d = %#v", i, am)
		}
	}
}

func TestAutoAttendantBodyNewFlowIDs(t *testing.T) {
	f := testFullFlows()
	f.Default.ID = types.StringUnknown()
	f.AfterHours.ID = types.StringUnknown()
	m := testAutoAttendantModel(t, f)
	body, diags := autoAttendantBodyFromModel(context.Background(), &m)
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	def := body["DefaultCallFlow"].(map[string]any)["Id"].(string)
	ah := body["CallFlows"].([]any)[0].(map[string]any)["Id"].(string)
	if !guidV4.MatchString(def) || !guidV4.MatchString(ah) || def == ah {
		t.Errorf("ids = %q, %q", def, ah)
	}
	if got := body["CallHandlingAssociations"].([]any)[0].(map[string]any)["CallFlowId"]; got != ah {
		t.Errorf("after hours CallFlowId = %v; want %s", got, ah)
	}
}

// testCapturedAutoAttendant is a stored auto attendant in the shape Teams returns.
func testCapturedAutoAttendant() map[string]any {
	menuNulls := func(m map[string]any) map[string]any {
		m["DialByNameEnabled"] = nil
		m["DirectorySearchMethod"] = nil
		return m
	}
	flow := func(id, name, greeting string, opts ...any) map[string]any {
		var greetings []any
		if greeting != "" {
			greetings = []any{map[string]any{"ActiveType": "TextToSpeech", "TextToSpeechPrompt": greeting, "AudioFilePrompt": nil}}
		} else {
			greetings = []any{}
		}
		return map[string]any{
			"Id":                     id,
			"Name":                   name,
			"Greetings":              greetings,
			"ForceListenMenuEnabled": nil,
			"Menu":                   menuNulls(map[string]any{"Name": name + " menu", "Prompts": []any{}, "MenuOptions": opts}),
		}
	}
	disconnect := map[string]any{"Action": "DisconnectCall", "DtmfResponse": "Automatic", "CallTarget": nil, "Prompt": nil, "VoiceResponses": nil, "AgentTarget": nil, "Description": nil}

	def := flow("flow-default", "tf main default", "Welcome",
		map[string]any{"Action": "TransferCallToTarget", "DtmfResponse": "Tone1", "CallTarget": map[string]any{"Id": "tel:+18005550101", "Type": "ExternalPstn", "EnableTranscription": nil}, "Prompt": nil, "AgentTarget": nil, "Description": nil},
		map[string]any{"Action": "TransferCallToTarget", "DtmfResponse": "Tone2", "CallTarget": map[string]any{"Id": "+18005550102", "Type": "ExternalPstn"}, "Prompt": nil, "AgentTarget": nil, "Description": nil},
		map[string]any{"Action": "DisconnectCall", "DtmfResponse": "Tone3", "CallTarget": nil, "Prompt": nil},
	)
	def["Menu"].(map[string]any)["Prompts"] = []any{map[string]any{"ActiveType": "TextToSpeech", "TextToSpeechPrompt": "Press 1 for sales, press 2 for support."}}

	return map[string]any{
		"Id":                       "aa-1",
		"Name":                     "tf main",
		"LanguageId":               "en-US",
		"TimeZoneId":               "Central Standard Time",
		"VoiceId":                  "Female",
		"VoiceResponseEnabled":     false,
		"DialByNameResourceId":     nil,
		"Description":              nil,
		"AgentTarget":              nil,
		"ApplicationInstances":     []any{"ra-1"},
		"AuthorizedUsers":          []any{},
		"DefaultCallFlow":          def,
		"Operator":                 nil,
		"Schedules":                []any{map[string]any{"Id": "sched-weekly"}, map[string]any{"Id": "sched-xmas"}},
		"DirectoryLookupScope":     nil,
		"ExclusionScope":           nil,
		"InclusionScope":           nil,
		"UserNameExtension":        nil,
		"EnableMainlineAttendant":  nil,
		"MainlineAttendantEnabled": nil,
		"CallFlows": []any{
			flow("flow-h2", "tf main holiday 2", "", disconnect),
			flow("flow-ah", "tf main after hours", "We are closed.", disconnect),
			flow("flow-h1", "tf main holiday 1", "Happy holidays", disconnect),
		},
		"CallHandlingAssociations": []any{
			map[string]any{"Type": "Holiday", "ScheduleId": "sched-ny", "CallFlowId": "flow-h2", "Enabled": true, "Priority": float64(3)},
			map[string]any{"Type": "AfterHours", "ScheduleId": "sched-weekly", "CallFlowId": "flow-ah", "Enabled": true, "Priority": float64(1)},
			map[string]any{"Type": "Holiday", "ScheduleId": "sched-xmas", "CallFlowId": "flow-h1", "Enabled": true, "Priority": float64(2)},
		},
	}
}

func TestAutoAttendantModelFromObject(t *testing.T) {
	ctx := context.Background()
	prior := testFullFlows()
	prior.Holidays[1].ID = types.StringValue("flow-h2")
	m := testAutoAttendantModel(t, prior)

	if diags := autoAttendantModelFromObject(ctx, testCapturedAutoAttendant(), &m); diags.HasError() {
		t.Fatalf("autoAttendantModelFromObject diagnostics: %v", diags)
	}
	strs := []struct {
		name      string
		got, want types.String
	}{
		{"id", m.ID, types.StringValue("aa-1")},
		{"name", m.Name, types.StringValue("tf main")},
		{"language_id", m.LanguageID, types.StringValue("en-US")},
		{"time_zone_id", m.TimeZoneID, types.StringValue("Central Standard Time")},
		{"voice_id", m.VoiceID, types.StringValue("female")},
	}
	for _, s := range strs {
		if !s.got.Equal(s.want) {
			t.Errorf("%s = %s; want %s", s.name, s.got, s.want)
		}
	}
	if got := listStrings(m.ApplicationInstances); !reflect.DeepEqual(got, []string{"ra-1"}) {
		t.Errorf("application_instances = %v", got)
	}

	f, d := m.flows(ctx)
	if d.HasError() {
		t.Fatalf("flows diagnostics: %v", d)
	}
	wantDefault := aaCallFlowModel{
		ID:         types.StringValue("flow-default"),
		Greeting:   types.StringValue("Welcome"),
		MenuPrompt: types.StringValue("Press 1 for sales, press 2 for support."),
		MenuOption: []aaMenuOptionModel{
			testOption("1", "transfer", "tel:+18005550101", "external_pstn", ""),
			testOption("2", "transfer", "tel:+18005550102", "external_pstn", ""),
			testOption("3", "disconnect", "", "", ""),
		},
	}
	if !reflect.DeepEqual(f.Default, wantDefault) {
		t.Errorf("default_call_flow = %+v\nwant %+v", f.Default, wantDefault)
	}
	wantAH := aaScheduledCallFlowModel{
		ID:         types.StringValue("flow-ah"),
		ScheduleID: types.StringValue("sched-weekly"),
		Greeting:   types.StringValue("We are closed."),
		MenuPrompt: types.StringNull(),
		MenuOption: []aaMenuOptionModel{testOption("automatic", "disconnect", "", "", "")},
	}
	if f.AfterHours == nil || !reflect.DeepEqual(*f.AfterHours, wantAH) {
		t.Errorf("after_hours = %+v\nwant %+v", f.AfterHours, wantAH)
	}
	if len(f.Holidays) != 2 {
		t.Fatalf("holiday = %+v", f.Holidays)
	}
	wantH := []struct{ id, schedule, greeting string }{
		{"flow-h1", "sched-xmas", "Happy holidays"},
		{"flow-h2", "sched-ny", ""},
	}
	for i, w := range wantH {
		h := f.Holidays[i]
		greeting := types.StringNull()
		if w.greeting != "" {
			greeting = types.StringValue(w.greeting)
		}
		if h.ID.ValueString() != w.id || h.ScheduleID.ValueString() != w.schedule || !h.Greeting.Equal(greeting) {
			t.Errorf("holiday[%d] = %+v; want id %s schedule %s greeting %q", i, h, w.id, w.schedule, w.greeting)
		}
		if want := []aaMenuOptionModel{testOption("automatic", "disconnect", "", "", "")}; !reflect.DeepEqual(h.MenuOption, want) {
			t.Errorf("holiday[%d].menu_option = %+v", i, h.MenuOption)
		}
	}
}

func TestAutoAttendantModelFromObjectImport(t *testing.T) {
	ctx := context.Background()
	m := autoAttendantModel{
		ID:                   types.StringValue("aa-1"),
		DefaultCallFlow:      types.ObjectNull(aaCallFlowAttrTypes),
		AfterHours:           types.ObjectNull(aaScheduledCallFlowAttrTypes),
		Holiday:              types.ListNull(types.ObjectType{AttrTypes: aaScheduledCallFlowAttrTypes}),
		ApplicationInstances: types.ListNull(types.StringType),
	}
	if diags := autoAttendantModelFromObject(ctx, testCapturedAutoAttendant(), &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	f, _ := m.flows(ctx)
	if got := f.Default.MenuOption[1].TargetID; !got.Equal(types.StringValue("+18005550102")) {
		t.Errorf("target_id without prior = %s", got)
	}
	if len(f.Holidays) != 2 || f.AfterHours == nil {
		t.Errorf("flows = %+v", f)
	}
}

func TestAutoAttendantModelFromObjectNoSchedules(t *testing.T) {
	ctx := context.Background()
	obj := testCapturedAutoAttendant()
	obj["CallFlows"] = []any{}
	obj["CallHandlingAssociations"] = []any{}
	m := testAutoAttendantModel(t, testFullFlows())
	if diags := autoAttendantModelFromObject(ctx, obj, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if !m.AfterHours.IsNull() {
		t.Errorf("after_hours = %s; want null", m.AfterHours)
	}
	if m.Holiday.IsNull() || len(m.Holiday.Elements()) != 0 {
		t.Errorf("holiday = %s; want empty list", m.Holiday)
	}
}

func TestAutoAttendantModelFromObjectUnknownEnums(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(map[string]any)
		summary string
	}{
		{"action", func(o map[string]any) {
			opt := o["DefaultCallFlow"].(map[string]any)["Menu"].(map[string]any)["MenuOptions"].([]any)[0].(map[string]any)
			opt["Action"] = "TransferCallToVoicemail"
		}, "Unexpected DefaultCallFlow.Menu.MenuOptions[0].Action value"},
		{"dtmf", func(o map[string]any) {
			opt := o["DefaultCallFlow"].(map[string]any)["Menu"].(map[string]any)["MenuOptions"].([]any)[1].(map[string]any)
			opt["DtmfResponse"] = float64(2)
		}, "Unexpected DefaultCallFlow.Menu.MenuOptions[1].DtmfResponse value"},
		{"target type", func(o map[string]any) {
			opt := o["DefaultCallFlow"].(map[string]any)["Menu"].(map[string]any)["MenuOptions"].([]any)[0].(map[string]any)
			opt["CallTarget"] = map[string]any{"Id": "x", "Type": "Bogus"}
		}, "Unexpected DefaultCallFlow.Menu.MenuOptions[0].CallTarget.Type value"},
		{"voice", func(o map[string]any) { o["VoiceId"] = "Robot" }, "Unexpected VoiceId value"},
		{"greeting type", func(o map[string]any) {
			o["DefaultCallFlow"].(map[string]any)["Greetings"] = []any{map[string]any{"ActiveType": "Bogus"}}
		}, "Unexpected DefaultCallFlow.Greetings.ActiveType value"},
		{"audio greeting without id", func(o map[string]any) {
			o["DefaultCallFlow"].(map[string]any)["Greetings"] = []any{map[string]any{"ActiveType": "AudioFile"}}
		}, "Unexpected DefaultCallFlow.Greetings.AudioFilePrompt value"},
		{"announcement prompt type", func(o map[string]any) {
			opt := o["DefaultCallFlow"].(map[string]any)["Menu"].(map[string]any)["MenuOptions"].([]any)[0].(map[string]any)
			opt["Prompt"] = map[string]any{"ActiveType": "Bogus"}
		}, "Unexpected DefaultCallFlow.Menu.MenuOptions[0].Prompt.ActiveType value"},
		{"association type", func(o map[string]any) {
			o["CallHandlingAssociations"].([]any)[0].(map[string]any)["Type"] = "Unknown"
		}, "Unexpected CallHandlingAssociations[2].Type value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := testCapturedAutoAttendant()
			tc.mutate(obj)
			m := testAutoAttendantModel(t, testFullFlows())
			diags := autoAttendantModelFromObject(context.Background(), obj, &m)
			if !diags.HasError() {
				t.Fatal("expected an error")
			}
			if got := diags.Errors()[0].Summary(); got != tc.summary {
				t.Errorf("summary = %q; want %q", got, tc.summary)
			}
		})
	}
}

func TestValidateAutoAttendant(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		mutate  func(*aaFlows)
		wantErr bool
	}{
		{"full config", func(*aaFlows) {}, false},
		{"transfer without target", func(f *aaFlows) {
			f.Default.MenuOption[0] = testOption("1", "transfer", "", "", "")
		}, true},
		{"transfer without target type", func(f *aaFlows) {
			f.Default.MenuOption[0] = testOption("1", "transfer", "tel:+18005550101", "", "")
		}, true},
		{"disconnect with target", func(f *aaFlows) {
			f.AfterHours.MenuOption[0] = testOption("automatic", "disconnect", "tel:+18005550101", "", "")
		}, true},
		{"operator with target type", func(f *aaFlows) {
			f.Default.MenuOption[1] = testOption("2", "transfer_to_operator", "", "user", "")
		}, true},
		{"announcement without text", func(f *aaFlows) {
			f.Holidays[1].MenuOption[0] = testOption("1", "announcement", "", "", "")
		}, true},
		{"announcement text on disconnect", func(f *aaFlows) {
			f.Holidays[0].MenuOption[0] = testOption("automatic", "disconnect", "", "", "x")
		}, true},
		{"duplicate keys", func(f *aaFlows) {
			f.Default.MenuOption[1] = testOption("1", "transfer_to_operator", "", "", "")
		}, true},
		{"disconnect on a key press", func(f *aaFlows) {
			f.Default.MenuOption[1] = testOption("2", "disconnect", "", "", "")
		}, true},
		{"automatic with others", func(f *aaFlows) {
			f.Default.MenuOption = append(f.Default.MenuOption, testOption("automatic", "disconnect", "", "", ""))
		}, true},
		{"unknown action skipped", func(f *aaFlows) {
			f.Default.MenuOption[0].Action = types.StringUnknown()
			f.Default.MenuOption[0].TargetID = types.StringNull()
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := testFullFlows()
			tc.mutate(&f)
			m := testAutoAttendantModel(t, f)
			if got := validateAutoAttendant(ctx, &m).HasError(); got != tc.wantErr {
				t.Errorf("HasError = %v; want %v", got, tc.wantErr)
			}
		})
	}

	t.Run("missing default call flow", func(t *testing.T) {
		m := testAutoAttendantModel(t, testFullFlows())
		m.DefaultCallFlow = types.ObjectNull(aaCallFlowAttrTypes)
		if !validateAutoAttendant(ctx, &m).HasError() {
			t.Error("expected an error")
		}
	})
}

func TestOverlayObject(t *testing.T) {
	cur := map[string]any{
		"Id":          "aa-1",
		"Name":        "old",
		"Description": nil,
		"DefaultCallFlow": map[string]any{
			"Id": "f0", "Name": "old default", "ForceListenMenuEnabled": nil,
			"Menu": map[string]any{"Name": "m", "DialByNameEnabled": false, "MenuOptions": []any{map[string]any{"Action": "DisconnectCall", "DtmfResponse": "Tone9"}}},
		},
		"CallFlows": []any{
			map[string]any{"Id": "f1", "Name": "old ah", "ForceListenMenuEnabled": false},
			map[string]any{"Id": "f-gone", "Name": "removed"},
		},
	}
	body := map[string]any{
		"Name": "new",
		"DefaultCallFlow": map[string]any{
			"Id": "f0", "Name": "new default",
			"Menu": map[string]any{"Name": "m2", "MenuOptions": []any{map[string]any{"Action": "DisconnectCall", "DtmfResponse": "Automatic"}}},
		},
		"CallFlows": []any{
			map[string]any{"Id": "F1", "Name": "new ah"},
			map[string]any{"Id": "f2", "Name": "new holiday"},
		},
	}
	got := overlayObject(cur, body)
	want := map[string]any{
		"Id":          "aa-1",
		"Name":        "new",
		"Description": nil,
		"DefaultCallFlow": map[string]any{
			"Id": "f0", "Name": "new default", "ForceListenMenuEnabled": nil,
			"Menu": map[string]any{"Name": "m2", "DialByNameEnabled": false, "MenuOptions": []any{map[string]any{"Action": "DisconnectCall", "DtmfResponse": "Automatic"}}},
		},
		"CallFlows": []any{
			map[string]any{"Id": "F1", "Name": "new ah", "ForceListenMenuEnabled": false},
			map[string]any{"Id": "f2", "Name": "new holiday"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("overlayObject = %#v\nwant %#v", got, want)
	}
	if cur["Name"] != "old" {
		t.Error("input was modified")
	}
}

func TestAutoAttendantUpdateBody(t *testing.T) {
	cur := map[string]any{
		"Id":        "aa-1",
		"Name":      "old",
		"VoiceId":   "Female",
		"Schedules": []any{map[string]any{"Id": "s1"}},
	}
	body := map[string]any{"Name": "new", "TimeZoneId": "UTC"}
	got := autoAttendantUpdateBody(cur, body)
	want := map[string]any{"Id": "aa-1", "Name": "new", "VoiceId": "Female", "TimeZoneId": "UTC"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("autoAttendantUpdateBody = %#v; want %#v", got, want)
	}
	if _, ok := cur["Schedules"]; !ok {
		t.Error("input was modified")
	}
}

func TestValidateAAMenuUnknownTargetSkipped(t *testing.T) {
	for _, action := range []string{"disconnect", "transfer_to_operator"} {
		o := testOption("automatic", action, "", "", "")
		if action != "disconnect" {
			o.Key = types.StringValue("1")
		}
		o.TargetID = types.StringUnknown()
		o.TargetType = types.StringUnknown()
		if diags := validateAAMenu(path.Root("default_call_flow"), []aaMenuOptionModel{o}); diags.HasError() {
			t.Errorf("%s with unknown target: %v", action, diags)
		}
		o.TargetID = types.StringValue("x")
		o.TargetType = types.StringValue("user")
		if diags := validateAAMenu(path.Root("default_call_flow"), []aaMenuOptionModel{o}); len(diags.Errors()) != 2 {
			t.Errorf("%s with known target: %v", action, diags)
		}
	}
}

func TestUnwrapAutoAttendant(t *testing.T) {
	inner := map[string]any{"Id": "aa-1", "Name": "n"}
	for name, in := range map[string][]map[string]any{
		"wrapped": {{"AutoAttendant": inner}},
		"bare":    {inner},
	} {
		if got := autoAttendantKind.unwrap(in); !reflect.DeepEqual(got, inner) {
			t.Errorf("%s: unwrap = %#v", name, got)
		}
	}
}

func TestIsAutoAttendantGone(t *testing.T) {
	if !isAutoAttendantGone(&teamsapi.APIError{Status: 404, Message: "Not Found"}) {
		t.Error("404 should be gone")
	}
	if isAutoAttendantGone(&teamsapi.APIError{Status: 400, Message: "bad request"}) || isAutoAttendantGone(nil) {
		t.Error("400 and nil should not be gone")
	}
}

func audioPrompt(id string) map[string]any {
	return map[string]any{"ActiveType": "AudioFile", "AudioFilePrompt": map[string]any{"Id": id}}
}

// testAudioFlows is a default flow whose greeting, menu prompt and key 2 announcement are audio files.
func testAudioFlows() aaFlows {
	announce := testOption("2", "announcement", "", "", "")
	announce.AnnouncementAudioFileID = types.StringValue("file-hours")
	return aaFlows{Default: aaCallFlowModel{
		ID:                    types.StringValue("flow-default"),
		Greeting:              types.StringNull(),
		GreetingAudioFileID:   types.StringValue("file-greeting"),
		MenuPrompt:            types.StringNull(),
		MenuPromptAudioFileID: types.StringValue("file-menu"),
		MenuOption: []aaMenuOptionModel{
			testOption("1", "transfer", "tel:+18005550101", "external_pstn", ""),
			announce,
			testOption("3", "announcement", "", "", "Our hours are 8 to 5."),
		},
	}}
}

func TestAutoAttendantBodyAudioPrompts(t *testing.T) {
	m := testAutoAttendantModel(t, testAudioFlows())
	body, diags := autoAttendantBodyFromModel(context.Background(), &m)
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	def := body["DefaultCallFlow"].(map[string]any)
	if got, want := def["Greetings"], []any{audioPrompt("file-greeting")}; !reflect.DeepEqual(got, want) {
		t.Errorf("Greetings = %#v; want %#v", got, want)
	}
	menu := def["Menu"].(map[string]any)
	if got, want := menu["Prompts"], []any{audioPrompt("file-menu")}; !reflect.DeepEqual(got, want) {
		t.Errorf("Prompts = %#v; want %#v", got, want)
	}
	opts := menu["MenuOptions"].([]any)
	if _, ok := opts[0].(map[string]any)["Prompt"]; ok {
		t.Errorf("transfer option has a Prompt: %#v", opts[0])
	}
	if got, want := opts[1].(map[string]any)["Prompt"], audioPrompt("file-hours"); !reflect.DeepEqual(got, want) {
		t.Errorf("audio announcement Prompt = %#v; want %#v", got, want)
	}
	if got, want := opts[2].(map[string]any)["Prompt"], tts("Our hours are 8 to 5."); !reflect.DeepEqual(got, want) {
		t.Errorf("text announcement Prompt = %#v; want %#v", got, want)
	}
}

func TestAutoAttendantModelFromObjectAudioPrompts(t *testing.T) {
	ctx := context.Background()
	readBack := func(id, name string) map[string]any {
		return map[string]any{"ActiveType": "AudioFile", "TextToSpeechPrompt": nil, "AudioFilePrompt": map[string]any{
			"Id": id, "FileName": name, "DownloadUri": "https://example.invalid/" + id,
		}}
	}
	obj := testCapturedAutoAttendant()
	def := obj["DefaultCallFlow"].(map[string]any)
	def["Greetings"] = []any{readBack("FILE-GREETING", "greeting.wav")}
	menu := def["Menu"].(map[string]any)
	menu["Prompts"] = []any{readBack("file-menu", "menu.wav")}
	menu["MenuOptions"] = []any{
		map[string]any{"Action": "Announcement", "DtmfResponse": "Tone2", "CallTarget": nil, "Prompt": readBack("file-hours", "hours.wav")},
		map[string]any{"Action": "Announcement", "DtmfResponse": "Tone3", "CallTarget": nil, "Prompt": tts("Our hours are 8 to 5.")},
	}

	m := testAutoAttendantModel(t, testAudioFlows())
	if diags := autoAttendantModelFromObject(ctx, obj, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	f, _ := m.flows(ctx)
	announce := testOption("2", "announcement", "", "", "")
	announce.AnnouncementAudioFileID = types.StringValue("file-hours")
	want := aaCallFlowModel{
		ID:                    types.StringValue("flow-default"),
		Greeting:              types.StringNull(),
		GreetingAudioFileID:   types.StringValue("file-greeting"),
		MenuPrompt:            types.StringNull(),
		MenuPromptAudioFileID: types.StringValue("file-menu"),
		MenuOption:            []aaMenuOptionModel{announce, testOption("3", "announcement", "", "", "Our hours are 8 to 5.")},
	}
	if !reflect.DeepEqual(f.Default, want) {
		t.Errorf("default_call_flow = %+v\nwant %+v", f.Default, want)
	}

	t.Run("without prior", func(t *testing.T) {
		m := testAutoAttendantModel(t, aaFlows{})
		if diags := autoAttendantModelFromObject(ctx, obj, &m); diags.HasError() {
			t.Fatalf("diagnostics: %v", diags)
		}
		f, _ := m.flows(ctx)
		if !f.Default.GreetingAudioFileID.Equal(types.StringValue("FILE-GREETING")) || !f.Default.Greeting.IsNull() {
			t.Errorf("greeting = %s / %s", f.Default.Greeting, f.Default.GreetingAudioFileID)
		}
	})
}

func TestValidateAutoAttendantAudio(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		mutate  func(*aaFlows)
		wantErr bool
	}{
		{"audio config", func(*aaFlows) {}, false},
		{"greeting text and audio", func(f *aaFlows) { f.Default.Greeting = types.StringValue("Welcome") }, true},
		{"menu prompt text and audio", func(f *aaFlows) { f.Default.MenuPrompt = types.StringValue("Press 2") }, true},
		{"unknown greeting audio skipped", func(f *aaFlows) {
			f.Default.Greeting = types.StringValue("Welcome")
			f.Default.GreetingAudioFileID = types.StringUnknown()
		}, false},
		{"announcement text and audio", func(f *aaFlows) {
			f.Default.MenuOption[1].Announcement = types.StringValue("x")
		}, true},
		{"announcement neither", func(f *aaFlows) {
			f.Default.MenuOption[1].AnnouncementAudioFileID = types.StringNull()
		}, true},
		{"announcement audio unknown", func(f *aaFlows) {
			f.Default.MenuOption[1].AnnouncementAudioFileID = types.StringUnknown()
		}, false},
		{"audio on transfer", func(f *aaFlows) {
			f.Default.MenuOption[0].AnnouncementAudioFileID = types.StringValue("file-hours")
		}, true},
		{"after hours greeting text and audio", func(f *aaFlows) {
			f.AfterHours = &aaScheduledCallFlowModel{
				ID: types.StringValue("flow-ah"), ScheduleID: types.StringValue("s"),
				Greeting: types.StringValue("Closed"), GreetingAudioFileID: types.StringValue("file-closed"),
				MenuPrompt: types.StringNull(), MenuPromptAudioFileID: types.StringNull(),
				MenuOption: []aaMenuOptionModel{testOption("automatic", "disconnect", "", "", "")},
			}
		}, true},
		{"holiday menu prompt text and audio", func(f *aaFlows) {
			f.Holidays = []aaScheduledCallFlowModel{{
				ID: types.StringValue("flow-h"), ScheduleID: types.StringValue("s"),
				Greeting: types.StringNull(), GreetingAudioFileID: types.StringNull(),
				MenuPrompt: types.StringValue("x"), MenuPromptAudioFileID: types.StringValue("file-x"),
				MenuOption: []aaMenuOptionModel{testOption("automatic", "disconnect", "", "", "")},
			}}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := testAudioFlows()
			tc.mutate(&f)
			m := testAutoAttendantModel(t, f)
			if got := validateAutoAttendant(ctx, &m).HasError(); got != tc.wantErr {
				t.Errorf("HasError = %v; want %v", got, tc.wantErr)
			}
		})
	}
}

func TestAutoAttendantUpdateBodyAudioFileNames(t *testing.T) {
	stored := func() map[string]any {
		return map[string]any{
			"Id": "aa-1",
			"DefaultCallFlow": map[string]any{
				"Id": "flow-default",
				"Greetings": []any{map[string]any{"ActiveType": "AudioFile", "AudioFilePrompt": map[string]any{
					"Id": "FILE-GREETING", "FileName": "greeting.wav", "DownloadUri": "https://example.invalid/g",
				}}},
			},
		}
	}
	cur := stored()
	body := map[string]any{
		"DefaultCallFlow": map[string]any{
			"Id":        "flow-default",
			"Greetings": []any{audioPrompt("file-greeting")},
			"Menu":      map[string]any{"Prompts": []any{audioPrompt("file-new")}},
		},
	}
	got := autoAttendantUpdateBody(cur, body)
	def := got["DefaultCallFlow"].(map[string]any)
	wantGreeting := []any{map[string]any{"ActiveType": "AudioFile", "AudioFilePrompt": map[string]any{"Id": "file-greeting", "FileName": "greeting.wav"}}}
	if !reflect.DeepEqual(def["Greetings"], wantGreeting) {
		t.Errorf("Greetings = %#v; want %#v", def["Greetings"], wantGreeting)
	}
	if got, want := def["Menu"].(map[string]any)["Prompts"], []any{audioPrompt("file-new")}; !reflect.DeepEqual(got, want) {
		t.Errorf("Prompts = %#v; want %#v (no stored name for a new file)", got, want)
	}
	if !reflect.DeepEqual(cur, stored()) {
		t.Error("cur was modified")
	}
	if !reflect.DeepEqual(body["DefaultCallFlow"].(map[string]any)["Greetings"], []any{audioPrompt("file-greeting")}) {
		t.Error("body was modified")
	}
}
