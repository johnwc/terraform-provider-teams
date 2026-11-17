package provider

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
)

func TestWireEnums(t *testing.T) {
	cases := []struct {
		name string
		enum wireEnum
		tf   string
		wire string
		num  int
	}{
		{"routing attendant", routingMethods, "attendant", "Attendant", 0},
		{"routing serial", routingMethods, "serial", "Serial", 1},
		{"routing round_robin", routingMethods, "round_robin", "RoundRobin", 2},
		{"routing longest_idle", routingMethods, "longest_idle", "LongestIdle", 3},
		{"overflow disconnect_with_busy", overflowActions, "disconnect_with_busy", "DisconnectWithBusy", 0},
		{"overflow forward", overflowActions, "forward", "Forward", 1},
		{"overflow voicemail", overflowActions, "voicemail", "Voicemail", 2},
		{"overflow shared_voicemail", overflowActions, "shared_voicemail", "SharedVoicemail", 3},
		{"timeout disconnect", timeoutActions, "disconnect", "Disconnect", 0},
		{"timeout forward", timeoutActions, "forward", "Forward", 1},
		{"timeout voicemail", timeoutActions, "voicemail", "Voicemail", 2},
		{"timeout shared_voicemail", timeoutActions, "shared_voicemail", "SharedVoicemail", 3},
		{"no_agent queue", noAgentActions, "queue", "Queue", 0},
		{"no_agent disconnect", noAgentActions, "disconnect", "Disconnect", 1},
		{"no_agent forward", noAgentActions, "forward", "Forward", 2},
		{"no_agent voicemail", noAgentActions, "voicemail", "Voicemail", 3},
		{"no_agent shared_voicemail", noAgentActions, "shared_voicemail", "SharedVoicemail", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := tc.enum.toWire(tc.tf); !ok || got != tc.wire {
				t.Errorf("toWire(%q) = %q, %v; want %q", tc.tf, got, ok, tc.wire)
			}
			for _, in := range []any{float64(tc.num), tc.num, int64(tc.num), tc.wire} {
				if got, ok := tc.enum.fromWire(in); !ok || got != tc.tf {
					t.Errorf("fromWire(%#v) = %q, %v; want %q", in, got, ok, tc.tf)
				}
			}
		})
	}
}

func TestWireEnumsRejectUnknown(t *testing.T) {
	for _, e := range []wireEnum{routingMethods, overflowActions, timeoutActions, noAgentActions} {
		if _, ok := e.toWire("bogus"); ok {
			t.Error("toWire(bogus) should fail")
		}
		for _, in := range []any{float64(99), float64(-1), 1.5, "Bogus", nil, true} {
			if got, ok := e.fromWire(in); ok {
				t.Errorf("fromWire(%#v) = %q; want failure", in, got)
			}
		}
	}
}

func TestUnwrapCallQueue(t *testing.T) {
	inner := map[string]any{"Identity": "q1", "Name": "Sales"}
	cases := []struct {
		name string
		in   []map[string]any
		want map[string]any
	}{
		{"wrapped", []map[string]any{{"CallQueue": inner}}, inner},
		{"bare", []map[string]any{inner}, inner},
		{"plural", []map[string]any{{"CallQueues": []any{inner}}}, inner},
		{"plural empty", []map[string]any{{"CallQueues": []any{}}}, nil},
		{"empty", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := callQueueKind.unwrap(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("callQueueKind.unwrap = %#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestCallQueueTargetValue(t *testing.T) {
	cases := []struct {
		name  string
		in    any
		prior types.String
		want  types.String
	}{
		{"object", map[string]any{"Id": "g", "Type": "MailBox", "ConfigurationEndpointType": nil}, types.StringNull(), types.StringValue("g")},
		{"null", nil, types.StringValue("old"), types.StringNull()},
		{"string", "tel:+18005550100", types.StringNull(), types.StringValue("tel:+18005550100")},
		{"phone keeps tel prefix", map[string]any{"Id": "+18005550100", "Type": "Phone"}, types.StringValue("tel:+18005550100"), types.StringValue("tel:+18005550100")},
		{"object without id", map[string]any{"Type": "User"}, types.StringNull(), types.StringNull()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := callQueueTargetValue(tc.in, tc.prior); !got.Equal(tc.want) {
				t.Errorf("callQueueTargetValue = %s; want %s", got, tc.want)
			}
		})
	}
}

// testCallQueueModel returns a model with the schema defaults applied.
func testCallQueueModel() callQueueModel {
	var m callQueueModel
	m.Name = types.StringValue("Sales")
	m.LanguageID = types.StringValue("en-US")
	m.RoutingMethod = types.StringValue("attendant")
	m.PresenceBasedRouting = types.BoolValue(false)
	m.ConferenceMode = types.BoolValue(false)
	m.AllowOptOut = types.BoolValue(false)
	m.AgentAlertTime = types.Int64Value(30)
	m.OverflowThreshold = types.Int64Value(50)
	m.OverflowAction = types.StringValue("disconnect_with_busy")
	m.EnableOverflowSharedVoicemailTranscription = types.BoolValue(false)
	m.TimeoutThreshold = types.Int64Value(1200)
	m.TimeoutAction = types.StringValue("disconnect")
	m.EnableTimeoutSharedVoicemailTranscription = types.BoolValue(false)
	m.NoAgentAction = types.StringValue("queue")
	m.EnableNoAgentSharedVoicemailTranscription = types.BoolValue(false)
	m.Users = types.ListNull(types.StringType)
	m.DistributionLists = types.ListNull(types.StringType)
	m.AuthorizedUsers = types.ListNull(types.StringType)
	return m
}

func TestCallQueueBodySharedVoicemailPrompt(t *testing.T) {
	m := testCallQueueModel()
	m.OverflowAction = types.StringValue("shared_voicemail")
	m.OverflowActionTarget = types.StringValue("g")
	m.OverflowSharedVoicemailTextToSpeechPrompt = types.StringValue("Leave a message")
	m.TimeoutAction = types.StringValue("forward")
	m.TimeoutActionTarget = types.StringValue("tel:+18005550100")
	m.TimeoutSharedVoicemailTextToSpeechPrompt = types.StringValue("ignored")
	m.NoAgentAction = types.StringNull()

	body, diags := callQueueBody(&m)
	if diags.HasError() {
		t.Fatalf("callQueueBody diagnostics: %v", diags)
	}
	want := []struct {
		key string
		val any
	}{
		{"OverflowAction", "SharedVoicemail"},
		{"OverflowSharedVoicemailTextToSpeechPrompt", "Leave a message"},
		{"OverflowActionTarget", "g"},
		{"TimeoutAction", "Forward"},
		{"TimeoutSharedVoicemailTextToSpeechPrompt", nil},
		{"TimeoutActionTarget", "tel:+18005550100"},
		{"NoAgentAction", "Queue"},
		{"NoAgentSharedVoicemailTextToSpeechPrompt", nil},
		{"NoAgentActionTarget", nil},
		{"RoutingMethod", "Attendant"},
		{"UseDefaultMusicOnHold", true},
		{"PresenceAwareRouting", false},
	}
	for _, w := range want {
		got, ok := body[w.key]
		if !ok {
			t.Errorf("body missing %s", w.key)
			continue
		}
		if !reflect.DeepEqual(got, w.val) {
			t.Errorf("body[%s] = %#v; want %#v", w.key, got, w.val)
		}
	}
	for _, k := range []string{"Agents", "DistributionLists", "AuthorizedUsers"} {
		if _, ok := body[k]; ok {
			t.Errorf("body[%s] should be omitted for a null list", k)
		}
	}
}

func TestCallQueueBodyAgents(t *testing.T) {
	m := testCallQueueModel()
	m.Users = types.ListValueMust(types.StringType, []attr.Value{types.StringValue("u1"), types.StringValue("u2")})
	body, diags := callQueueBody(&m)
	if diags.HasError() {
		t.Fatalf("callQueueBody diagnostics: %v", diags)
	}
	want := []any{
		map[string]any{"ObjectId": "u1", "OptIn": true},
		map[string]any{"ObjectId": "u2", "OptIn": true},
	}
	if !reflect.DeepEqual(body["Agents"], want) {
		t.Errorf("Agents = %#v; want %#v", body["Agents"], want)
	}
}

func TestCallQueueModelFromObject(t *testing.T) {
	obj := map[string]any{}
	obj["Identity"] = "0b9d5a1e-1111-2222-3333-444455556666"
	obj["Name"] = "Support"
	obj["LanguageId"] = "en-GB"
	obj["RoutingMethod"] = float64(3)
	obj["PresenceAwareRouting"] = false
	obj["ConferenceMode"] = true
	obj["AllowOptOut"] = true
	obj["AgentAlertTime"] = float64(45)
	obj["UseDefaultMusicOnHold"] = true
	obj["WelcomeTextToSpeechPrompt"] = nil
	obj["OverflowThreshold"] = float64(10)
	obj["OverflowAction"] = float64(3)
	obj["OverflowActionTarget"] = map[string]any{"Id": "g", "Type": "MailBox", "ConfigurationEndpointType": nil}
	obj["OverflowSharedVoicemailTextToSpeechPrompt"] = "Leave a message"
	obj["EnableOverflowSharedVoicemailTranscription"] = true
	obj["OverflowDisconnectTextToSpeechPrompt"] = nil
	obj["TimeoutThreshold"] = float64(600)
	obj["TimeoutAction"] = float64(1)
	obj["TimeoutActionTarget"] = map[string]any{"Id": "u9", "Type": "User", "ConfigurationEndpointType": nil}
	obj["TimeoutSharedVoicemailTextToSpeechPrompt"] = nil
	obj["EnableTimeoutSharedVoicemailTranscription"] = false
	obj["NoAgentAction"] = float64(0)
	obj["NoAgentActionTarget"] = nil
	obj["NoAgentSharedVoicemailTextToSpeechPrompt"] = nil
	obj["EnableNoAgentSharedVoicemailTranscription"] = false
	obj["DistributionLists"] = []any{"dl1"}
	obj["AuthorizedUsers"] = []any{}
	obj["Agents"] = []any{map[string]any{"ObjectId": "a1", "OptIn": true}, map[string]any{"ObjectId": "a2", "OptIn": false}}
	obj["ApplicationInstances"] = []any{"ra1"}
	obj["OboResourceAccountIds"] = []any{}

	m := testCallQueueModel()
	if diags := callQueueModelFromObject(obj, &m); diags.HasError() {
		t.Fatalf("callQueueModelFromObject diagnostics: %v", diags)
	}

	strs := []struct {
		name      string
		got, want types.String
	}{
		{"id", m.ID, types.StringValue("0b9d5a1e-1111-2222-3333-444455556666")},
		{"name", m.Name, types.StringValue("Support")},
		{"language_id", m.LanguageID, types.StringValue("en-GB")},
		{"routing_method", m.RoutingMethod, types.StringValue("longest_idle")},
		{"welcome_text_to_speech_prompt", m.WelcomeTextToSpeechPrompt, types.StringNull()},
		{"overflow_action", m.OverflowAction, types.StringValue("shared_voicemail")},
		{"overflow_action_target", m.OverflowActionTarget, types.StringValue("g")},
		{"overflow_shared_voicemail_text_to_speech_prompt", m.OverflowSharedVoicemailTextToSpeechPrompt, types.StringValue("Leave a message")},
		{"overflow_disconnect_text_to_speech_prompt", m.OverflowDisconnectTextToSpeechPrompt, types.StringNull()},
		{"timeout_action", m.TimeoutAction, types.StringValue("forward")},
		{"timeout_action_target", m.TimeoutActionTarget, types.StringValue("u9")},
		{"timeout_shared_voicemail_text_to_speech_prompt", m.TimeoutSharedVoicemailTextToSpeechPrompt, types.StringNull()},
		{"no_agent_action", m.NoAgentAction, types.StringValue("queue")},
		{"no_agent_action_target", m.NoAgentActionTarget, types.StringNull()},
	}
	for _, s := range strs {
		if !s.got.Equal(s.want) {
			t.Errorf("%s = %s; want %s", s.name, s.got, s.want)
		}
	}
	if m.PresenceBasedRouting.ValueBool() || !m.ConferenceMode.ValueBool() || !m.AllowOptOut.ValueBool() {
		t.Errorf("bools = %s/%s/%s", m.PresenceBasedRouting, m.ConferenceMode, m.AllowOptOut)
	}
	if !m.EnableOverflowSharedVoicemailTranscription.ValueBool() {
		t.Error("enable_overflow_shared_voicemail_transcription should be true")
	}
	if m.AgentAlertTime.ValueInt64() != 45 || m.OverflowThreshold.ValueInt64() != 10 || m.TimeoutThreshold.ValueInt64() != 600 {
		t.Errorf("ints = %s/%s/%s", m.AgentAlertTime, m.OverflowThreshold, m.TimeoutThreshold)
	}
	if got := listStrings(m.Users); !reflect.DeepEqual(got, []string{"a1", "a2"}) {
		t.Errorf("users = %v", got)
	}
	if got := listStrings(m.DistributionLists); !reflect.DeepEqual(got, []string{"dl1"}) {
		t.Errorf("distribution_lists = %v", got)
	}
	if !m.AuthorizedUsers.IsNull() {
		t.Errorf("authorized_users = %s; want null", m.AuthorizedUsers)
	}
	if got := listStrings(m.ApplicationInstances); !reflect.DeepEqual(got, []string{"ra1"}) {
		t.Errorf("application_instances = %v", got)
	}
	if m.OboResourceAccountIDs.IsNull() || len(m.OboResourceAccountIDs.Elements()) != 0 {
		t.Errorf("obo_resource_account_ids = %s; want empty list", m.OboResourceAccountIDs)
	}
}

func testStringList(ss ...string) types.List {
	vals := make([]attr.Value, 0, len(ss))
	for _, s := range ss {
		vals = append(vals, types.StringValue(s))
	}
	return types.ListValueMust(types.StringType, vals)
}

func TestCallQueueModelFromObjectAgentOrder(t *testing.T) {
	agents := []any{map[string]any{"ObjectId": "a1"}, map[string]any{"ObjectId": "a2"}}
	cases := []struct {
		name  string
		prior types.List
		want  []string
	}{
		{"same set keeps prior order", testStringList("a2", "a1"), []string{"a2", "a1"}},
		{"changed set uses server order", testStringList("a2", "a3"), []string{"a1", "a2"}},
		{"smaller prior uses server order", testStringList("a2"), []string{"a1", "a2"}},
		{"null prior uses server order", types.ListNull(types.StringType), []string{"a1", "a2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := testCallQueueModel()
			m.Users = tc.prior
			if diags := callQueueModelFromObject(map[string]any{"Identity": "q", "Agents": agents}, &m); diags.HasError() {
				t.Fatalf("callQueueModelFromObject diagnostics: %v", diags)
			}
			if got := listStrings(m.Users); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("users = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestCallQueueUpdateBody(t *testing.T) {
	newCur := func() map[string]any {
		return map[string]any{
			"Identity":          "q1",
			"Name":              "old",
			"Extra":             "keep",
			"Agents":            []any{map[string]any{"ObjectId": "a"}},
			"Users":             []any{"a"},
			"DistributionLists": []any{"d"},
			"AuthorizedUsers":   []any{"u"},
		}
	}
	set := func(s string) types.List { return testStringList(s) }
	null := types.ListNull(types.StringType)
	empty := types.ListValueMust(types.StringType, []attr.Value{})
	curAgents := []any{map[string]any{"ObjectId": "a"}}
	cleared := []any{}

	cases := []struct {
		name        string
		plan, state func(*callQueueModel)
		want        map[string]any
	}{
		{
			"null in plan and state keeps cur lists",
			func(*callQueueModel) {}, func(*callQueueModel) {},
			map[string]any{"Agents": curAgents, "Users": []any{"a"}, "DistributionLists": []any{"d"}, "AuthorizedUsers": []any{"u"}},
		},
		{
			"clear users and agents together",
			func(m *callQueueModel) { m.Users = null }, func(m *callQueueModel) { m.Users = set("a") },
			map[string]any{"Agents": cleared, "Users": cleared, "DistributionLists": []any{"d"}, "AuthorizedUsers": []any{"u"}},
		},
		{
			"clear distribution lists",
			func(m *callQueueModel) { m.DistributionLists = null }, func(m *callQueueModel) { m.DistributionLists = set("d") },
			map[string]any{"Agents": curAgents, "Users": []any{"a"}, "DistributionLists": cleared, "AuthorizedUsers": []any{"u"}},
		},
		{
			"clear authorized users",
			func(m *callQueueModel) { m.AuthorizedUsers = null }, func(m *callQueueModel) { m.AuthorizedUsers = set("u") },
			map[string]any{"Agents": curAgents, "Users": []any{"a"}, "DistributionLists": []any{"d"}, "AuthorizedUsers": cleared},
		},
		{
			"empty list in plan is written from the body, not cleared",
			func(m *callQueueModel) { m.DistributionLists = empty }, func(m *callQueueModel) { m.DistributionLists = set("d") },
			map[string]any{"Agents": curAgents, "Users": []any{"a"}, "DistributionLists": []string{}, "AuthorizedUsers": []any{"u"}},
		},
		{
			"null in plan with null state is not cleared",
			func(m *callQueueModel) { m.AuthorizedUsers = null }, func(m *callQueueModel) { m.AuthorizedUsers = null },
			map[string]any{"Agents": curAgents, "Users": []any{"a"}, "DistributionLists": []any{"d"}, "AuthorizedUsers": []any{"u"}},
		},
		{
			"all cleared",
			func(m *callQueueModel) {},
			func(m *callQueueModel) {
				m.Users, m.DistributionLists, m.AuthorizedUsers = set("a"), set("d"), set("u")
			},
			map[string]any{"Agents": cleared, "Users": cleared, "DistributionLists": cleared, "AuthorizedUsers": cleared},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, state := testCallQueueModel(), testCallQueueModel()
			tc.plan(&plan)
			tc.state(&state)
			body, diags := callQueueBody(&plan)
			if diags.HasError() {
				t.Fatalf("callQueueBody diagnostics: %v", diags)
			}
			cur := newCur()
			got := callQueueUpdateBody(cur, body, &plan, &state)
			for k, v := range tc.want {
				if !reflect.DeepEqual(got[k], v) {
					t.Errorf("%s = %#v; want %#v", k, got[k], v)
				}
			}
			if got["Name"] != "Sales" || got["Extra"] != "keep" || got["Identity"] != "q1" {
				t.Errorf("Name/Extra/Identity = %v/%v/%v; want Sales/keep/q1", got["Name"], got["Extra"], got["Identity"])
			}
			if !reflect.DeepEqual(cur, newCur()) {
				t.Error("cur was modified")
			}
		})
	}
}

func TestCallQueueModelFromObjectUnknownEnum(t *testing.T) {
	m := testCallQueueModel()
	diags := callQueueModelFromObject(map[string]any{"Identity": "q", "OverflowAction": float64(9)}, &m)
	if !diags.HasError() {
		t.Fatal("expected an error for an unknown OverflowAction")
	}
	if got := diags.Errors()[0].Summary(); got != "Unexpected OverflowAction value" {
		t.Errorf("summary = %q", got)
	}
}

func TestValidateCallQueue(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*callQueueModel)
		wantErr bool
	}{
		{"defaults", func(*callQueueModel) {}, false},
		{"longest idle with presence", func(m *callQueueModel) {
			m.RoutingMethod = types.StringValue("longest_idle")
			m.PresenceBasedRouting = types.BoolValue(true)
		}, true},
		{"forward without target", func(m *callQueueModel) { m.OverflowAction = types.StringValue("forward") }, true},
		{"shared voicemail without prompt", func(m *callQueueModel) {
			m.TimeoutAction = types.StringValue("shared_voicemail")
			m.TimeoutActionTarget = types.StringValue("g")
		}, true},
		{"prompt without shared voicemail", func(m *callQueueModel) {
			m.NoAgentSharedVoicemailTextToSpeechPrompt = types.StringValue("x")
		}, true},
		{"shared voicemail complete", func(m *callQueueModel) {
			m.NoAgentAction = types.StringValue("shared_voicemail")
			m.NoAgentActionTarget = types.StringValue("g")
			m.NoAgentSharedVoicemailTextToSpeechPrompt = types.StringValue("x")
		}, false},
		{"unknown action skipped", func(m *callQueueModel) { m.OverflowAction = types.StringUnknown() }, false},
		{"overflow disconnect_with_busy with target", func(m *callQueueModel) { m.OverflowActionTarget = types.StringValue("g") }, true},
		{"timeout disconnect with target", func(m *callQueueModel) { m.TimeoutActionTarget = types.StringValue("g") }, true},
		{"no agent queue with target", func(m *callQueueModel) { m.NoAgentActionTarget = types.StringValue("g") }, true},
		{"no agent default action with target", func(m *callQueueModel) {
			m.NoAgentAction = types.StringNull()
			m.NoAgentActionTarget = types.StringValue("g")
		}, true},
		{"unknown target skipped", func(m *callQueueModel) { m.OverflowActionTarget = types.StringUnknown() }, false},
		{"forward with target", func(m *callQueueModel) {
			m.OverflowAction = types.StringValue("forward")
			m.OverflowActionTarget = types.StringValue("tel:+18005550100")
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := testCallQueueModel()
			tc.mutate(&m)
			if got := validateCallQueue(&m).HasError(); got != tc.wantErr {
				t.Errorf("HasError = %v; want %v", got, tc.wantErr)
			}
		})
	}
}

func TestIsCallQueueGone(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404", &teamsapi.APIError{Status: 404, Message: "Not Found"}, true},
		{"400 invalid identity", &teamsapi.APIError{Status: 400, Message: "Invalid identity (x). Please check the identity."}, true},
		{"wrapped 400 invalid identity", fmt.Errorf("get: %w", &teamsapi.APIError{Status: 400, Message: "Invalid identity (x)"}), true},
		{"other 400", &teamsapi.APIError{Status: 400, Message: "Shared voicemail prompt should be null"}, false},
		{"plain error", errors.New("get failed: Invalid identity (x)"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCallQueueGone(tc.err); got != tc.want {
				t.Errorf("isCallQueueGone = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestCreateCallQueueBody_AgentsBecomeUsers(t *testing.T) {
	in := map[string]any{
		"Name":   "q",
		"Agents": []any{map[string]any{"ObjectId": "a", "OptIn": true}, map[string]any{"ObjectId": "b", "OptIn": true}},
	}
	out := createCallQueueBody(in)
	if _, ok := out["Agents"]; ok {
		t.Fatalf("Agents should be removed from the create body: %v", out)
	}
	users, ok := out["Users"].([]string)
	if !ok || len(users) != 2 || users[0] != "a" || users[1] != "b" {
		t.Fatalf("Users = %#v, want [a b]", out["Users"])
	}
	if _, ok := in["Agents"]; !ok {
		t.Fatalf("input body was modified")
	}
}

func TestCreateCallQueueBody_NoAgents(t *testing.T) {
	out := createCallQueueBody(map[string]any{"Name": "q"})
	if _, ok := out["Users"]; ok {
		t.Fatalf("Users should be absent when no agents are set: %v", out)
	}
}
