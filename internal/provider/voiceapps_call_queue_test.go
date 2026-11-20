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
	m.HideAuthorizedUsers = types.ListNull(types.StringType)
	m.OboResourceAccountIDs = types.ListNull(types.StringType)
	m.IsCallbackEnabled = types.BoolValue(false)
	m.NoAgentApplyTo = types.StringValue("all_calls")
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
	if !m.OboResourceAccountIDs.IsNull() {
		t.Errorf("obo_resource_account_ids = %s; want null", m.OboResourceAccountIDs)
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

func TestCallQueueBodyAudioFiles(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		m := testCallQueueModel()
		body, diags := callQueueBody(&m)
		if diags.HasError() {
			t.Fatalf("diagnostics: %v", diags)
		}
		for key, want := range map[string]any{
			"UseDefaultMusicOnHold":                  true,
			"MusicOnHoldAudioFileId":                 nil,
			"WelcomeMusicAudioFileId":                nil,
			"OverflowSharedVoicemailAudioFilePrompt": nil,
			"TimeoutSharedVoicemailAudioFilePrompt":  nil,
			"NoAgentSharedVoicemailAudioFilePrompt":  nil,
		} {
			if got, ok := body[key]; !ok || !reflect.DeepEqual(got, want) {
				t.Errorf("body[%s] = %#v (present %v); want %#v", key, got, ok, want)
			}
		}
	})
	t.Run("audio files", func(t *testing.T) {
		m := testCallQueueModel()
		m.WelcomeAudioFileID = types.StringValue("file-welcome")
		m.MusicOnHoldAudioFileID = types.StringValue("file-music")
		m.OverflowAction = types.StringValue("shared_voicemail")
		m.OverflowActionTarget = types.StringValue("g")
		m.OverflowSharedVoicemailAudioFileID = types.StringValue("file-vm")
		m.TimeoutAction = types.StringValue("forward")
		m.TimeoutActionTarget = types.StringValue("tel:+18005550100")
		m.TimeoutSharedVoicemailAudioFileID = types.StringValue("ignored")
		body, diags := callQueueBody(&m)
		if diags.HasError() {
			t.Fatalf("diagnostics: %v", diags)
		}
		for key, want := range map[string]any{
			"UseDefaultMusicOnHold":                     false,
			"MusicOnHoldAudioFileId":                    "file-music",
			"WelcomeMusicAudioFileId":                   "file-welcome",
			"WelcomeTextToSpeechPrompt":                 nil,
			"OverflowSharedVoicemailAudioFilePrompt":    "file-vm",
			"OverflowSharedVoicemailTextToSpeechPrompt": nil,
			"TimeoutSharedVoicemailAudioFilePrompt":     nil,
		} {
			if got, ok := body[key]; !ok || !reflect.DeepEqual(got, want) {
				t.Errorf("body[%s] = %#v (present %v); want %#v", key, got, ok, want)
			}
		}
	})
}

func TestCallQueueModelFromObjectAudioFiles(t *testing.T) {
	obj := map[string]any{
		"Identity":                               "q",
		"UseDefaultMusicOnHold":                  false,
		"MusicOnHoldAudioFileId":                 nil,
		"MusicOnHoldResourceId":                  "FILE-MUSIC",
		"WelcomeMusicAudioFileId":                "file-welcome",
		"OverflowSharedVoicemailAudioFilePrompt": "file-vm",
		"TimeoutSharedVoicemailAudioFilePrompt":  nil,
	}
	m := testCallQueueModel()
	m.MusicOnHoldAudioFileID = types.StringValue("file-music")
	if diags := callQueueModelFromObject(obj, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	strs := []struct {
		name      string
		got, want types.String
	}{
		{"music_on_hold_audio_file_id keeps prior case", m.MusicOnHoldAudioFileID, types.StringValue("file-music")},
		{"welcome_audio_file_id from WelcomeMusicAudioFileId", m.WelcomeAudioFileID, types.StringValue("file-welcome")},
		{"overflow_shared_voicemail_audio_file_id", m.OverflowSharedVoicemailAudioFileID, types.StringValue("file-vm")},
		{"timeout_shared_voicemail_audio_file_id", m.TimeoutSharedVoicemailAudioFileID, types.StringNull()},
		{"no_agent_shared_voicemail_audio_file_id", m.NoAgentSharedVoicemailAudioFileID, types.StringNull()},
	}
	for _, s := range strs {
		if !s.got.Equal(s.want) {
			t.Errorf("%s = %s; want %s", s.name, s.got, s.want)
		}
	}

	obj["WelcomeMusicResourceId"] = "file-welcome-2"
	obj["UseDefaultMusicOnHold"] = true
	m = testCallQueueModel()
	if diags := callQueueModelFromObject(obj, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if !m.WelcomeAudioFileID.Equal(types.StringValue("file-welcome-2")) {
		t.Errorf("welcome_audio_file_id = %s; want WelcomeMusicResourceId", m.WelcomeAudioFileID)
	}
	if !m.MusicOnHoldAudioFileID.IsNull() {
		t.Errorf("music_on_hold_audio_file_id = %s; want null with the default music", m.MusicOnHoldAudioFileID)
	}
}

func TestCallQueueUpdateBodyAudioResourceIDs(t *testing.T) {
	plan := testCallQueueModel()
	plan.MusicOnHoldAudioFileID = types.StringValue("file-new")
	body, diags := callQueueBody(&plan)
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	cur := map[string]any{"Identity": "q1", "MusicOnHoldResourceId": "file-old", "UseDefaultMusicOnHold": false}
	state := testCallQueueModel()
	got := callQueueUpdateBody(cur, body, &plan, &state)
	if got["MusicOnHoldResourceId"] != "file-new" || got["MusicOnHoldAudioFileId"] != "file-new" {
		t.Errorf("music ids = %v / %v; want file-new", got["MusicOnHoldResourceId"], got["MusicOnHoldAudioFileId"])
	}
	if _, ok := got["WelcomeMusicResourceId"]; ok {
		t.Error("WelcomeMusicResourceId should only be written when stored")
	}

	plan.MusicOnHoldAudioFileID = types.StringNull()
	body, _ = callQueueBody(&plan)
	got = callQueueUpdateBody(cur, body, &plan, &state)
	if got["UseDefaultMusicOnHold"] != true || got["MusicOnHoldResourceId"] != nil {
		t.Errorf("default music = %v / %v; want true / nil", got["UseDefaultMusicOnHold"], got["MusicOnHoldResourceId"])
	}
}

func TestValidateCallQueueAudio(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*callQueueModel)
		wantErr bool
	}{
		{"shared voicemail with audio", func(m *callQueueModel) {
			m.OverflowAction = types.StringValue("shared_voicemail")
			m.OverflowActionTarget = types.StringValue("g")
			m.OverflowSharedVoicemailAudioFileID = types.StringValue("file-vm")
		}, false},
		{"shared voicemail with text and audio", func(m *callQueueModel) {
			m.OverflowAction = types.StringValue("shared_voicemail")
			m.OverflowActionTarget = types.StringValue("g")
			m.OverflowSharedVoicemailAudioFileID = types.StringValue("file-vm")
			m.OverflowSharedVoicemailTextToSpeechPrompt = types.StringValue("x")
		}, true},
		{"shared voicemail with unknown audio", func(m *callQueueModel) {
			m.TimeoutAction = types.StringValue("shared_voicemail")
			m.TimeoutActionTarget = types.StringValue("g")
			m.TimeoutSharedVoicemailAudioFileID = types.StringUnknown()
		}, false},
		{"audio without shared voicemail", func(m *callQueueModel) {
			m.NoAgentSharedVoicemailAudioFileID = types.StringValue("file-vm")
		}, true},
		{"welcome text and audio", func(m *callQueueModel) {
			m.WelcomeTextToSpeechPrompt = types.StringValue("Hi")
			m.WelcomeAudioFileID = types.StringValue("file-welcome")
		}, true},
		{"welcome audio and music", func(m *callQueueModel) {
			m.WelcomeAudioFileID = types.StringValue("file-welcome")
			m.MusicOnHoldAudioFileID = types.StringValue("file-music")
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

func TestCreateCallQueueBody_NoAgents(t *testing.T) {
	out := createCallQueueBody(map[string]any{"Name": "q"})
	if _, ok := out["Users"]; ok {
		t.Fatalf("Users should be absent when no agents are set: %v", out)
	}
}

func TestSameIDs(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{"A", "b"}, []string{"B", "a"}, true},
		{[]string{"a"}, nil, false},
		{[]string{"a", "a"}, []string{"a", "b"}, false},
	}
	for _, tc := range cases {
		if got := sameIDs(tc.a, tc.b); got != tc.want {
			t.Errorf("sameIDs(%v, %v) = %v; want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCallQueueSystemPromptSuppression(t *testing.T) {
	m := testCallQueueModel()
	m.TimeoutAction = types.StringValue("shared_voicemail")
	m.TimeoutActionTarget = types.StringValue("g")
	m.TimeoutSharedVoicemailAudioFileID = types.StringValue("a1")
	m.EnableTimeoutSharedVoicemailSystemPromptSuppression = types.BoolValue(true)
	m.EnableNoAgentSharedVoicemailSystemPromptSuppression = types.BoolValue(false)
	if diags := validateCallQueue(&m); diags.HasError() {
		t.Fatalf("validateCallQueue: %v", diags)
	}
	body, diags := callQueueBody(&m)
	if diags.HasError() {
		t.Fatalf("callQueueBody diagnostics: %v", diags)
	}
	for key, want := range map[string]bool{
		"EnableTimeoutSharedVoicemailSystemPromptSuppression":  true,
		"EnableNoAgentSharedVoicemailSystemPromptSuppression":  false,
		"EnableOverflowSharedVoicemailSystemPromptSuppression": false,
	} {
		if got := body[key]; got != want {
			t.Errorf("body[%s] = %#v; want %v", key, got, want)
		}
	}

	m.NoAgentSharedVoicemailTextToSpeechPrompt = types.StringNull()
	m.EnableNoAgentSharedVoicemailSystemPromptSuppression = types.BoolValue(true)
	if !validateCallQueue(&m).HasError() {
		t.Error("suppression without shared_voicemail should fail validation")
	}

	var read callQueueModel
	callQueueModelFromObject(map[string]any{
		"EnableTimeoutSharedVoicemailSystemPromptSuppression": true,
		"EnableNoAgentSharedVoicemailSystemPromptSuppression": nil,
	}, &read)
	if !read.EnableTimeoutSharedVoicemailSystemPromptSuppression.ValueBool() {
		t.Error("timeout suppression did not read back true")
	}
	if v := read.EnableNoAgentSharedVoicemailSystemPromptSuppression; v.IsNull() || v.ValueBool() {
		t.Errorf("null suppression read back as %v; want false", v)
	}
}

func TestCallQueueChannel(t *testing.T) {
	m := testCallQueueModel()
	if q := callQueueChannelQuery(&m); q != "" {
		t.Errorf("query without channel = %q; want empty", q)
	}
	body, _ := callQueueBody(&m)
	if body["ThreadId"] != nil || body["ShouldOverwriteCallableChannelProperty"] != false {
		t.Errorf("body without channel: ThreadId=%#v Overwrite=%#v", body["ThreadId"], body["ShouldOverwriteCallableChannelProperty"])
	}

	m.ChannelID = types.StringValue("19:abc@thread.tacv2")
	if !validateCallQueue(&m).HasError() {
		t.Error("channel without owner and team group should fail validation")
	}
	m.ChannelUserObjectID = types.StringValue("owner-1")
	m.DistributionLists = types.ListValueMust(types.StringType, []attr.Value{types.StringValue("team-1")})
	m.OverwriteCallableChannel = types.BoolValue(true)
	if diags := validateCallQueue(&m); diags.HasError() {
		t.Fatalf("validateCallQueue: %v", diags)
	}
	if q := callQueueChannelQuery(&m); q != "?channelUserObjectId=owner-1" {
		t.Errorf("query = %q", q)
	}
	body, diags := callQueueBody(&m)
	if diags.HasError() {
		t.Fatalf("callQueueBody diagnostics: %v", diags)
	}
	if body["ThreadId"] != "19:abc@thread.tacv2" || body["ShouldOverwriteCallableChannelProperty"] != true {
		t.Errorf("body with channel: ThreadId=%#v Overwrite=%#v", body["ThreadId"], body["ShouldOverwriteCallableChannelProperty"])
	}

	read := m
	callQueueModelFromObject(map[string]any{"ThreadId": "19:abc@thread.tacv2"}, &read)
	if read.ChannelID.ValueString() != "19:abc@thread.tacv2" || read.ChannelUserObjectID.ValueString() != "owner-1" {
		t.Errorf("read back channel %v owner %v", read.ChannelID, read.ChannelUserObjectID)
	}

	m = testCallQueueModel()
	m.ChannelUserObjectID = types.StringValue("owner-1")
	if !validateCallQueue(&m).HasError() {
		t.Error("owner without channel should fail validation")
	}
}

// testCallQueueCallback enables a complete call back on the model.
func testCallQueueCallback(m *callQueueModel) {
	m.IsCallbackEnabled = types.BoolValue(true)
	m.ConferenceMode = types.BoolValue(true)
	m.CallbackRequestDtmf = types.StringValue("tone1")
	m.CallbackOfferTextToSpeechPrompt = types.StringValue("Press 1 for a call back")
	m.WaitTimeBeforeOfferingCallbackInSecond = types.Int64Value(60)
}

// checkCallQueueBody reports body keys that are missing or differ from want.
func checkCallQueueBody(t *testing.T, body map[string]any, want map[string]any) {
	t.Helper()
	for key, w := range want {
		if got, ok := body[key]; !ok || !reflect.DeepEqual(got, w) {
			t.Errorf("body[%s] = %#v (present %v); want %#v", key, got, ok, w)
		}
	}
}

func TestCallQueueBodyCallback(t *testing.T) {
	m := testCallQueueModel()
	body, diags := callQueueBody(&m)
	if diags.HasError() {
		t.Fatalf("callQueueBody diagnostics: %v", diags)
	}
	checkCallQueueBody(t, body, map[string]any{
		"IsCallbackEnabled":                               false,
		"CallbackRequestDtmf":                             nil,
		"WaitTimeBeforeOfferingCallbackInSecond":          nil,
		"NumberOfCallsInQueueBeforeOfferingCallback":      nil,
		"CallToAgentRatioThresholdBeforeOfferingCallback": nil,
		"CallbackOfferTextToSpeechPrompt":                 nil,
		"CallbackOfferAudioFilePromptResourceId":          nil,
		"CallbackEmailNotificationTarget":                 nil,
		"ServiceLevelThresholdResponseTimeInSecond":       nil,
	})

	testCallQueueCallback(&m)
	m.NumberOfCallsInQueueBeforeOfferingCallback = types.Int64Value(5)
	m.CallToAgentRatioThresholdBeforeOfferingCallback = types.Int64Value(3)
	m.CallbackEmailNotificationTarget = types.StringValue("g1")
	m.ServiceLevelThresholdResponseTimeInSecond = types.Int64Value(20)
	m.CallbackRequestDtmf = types.StringValue("pound")
	body, diags = callQueueBody(&m)
	if diags.HasError() {
		t.Fatalf("callQueueBody diagnostics: %v", diags)
	}
	checkCallQueueBody(t, body, map[string]any{
		"IsCallbackEnabled":                               true,
		"CallbackRequestDtmf":                             "TonePound",
		"WaitTimeBeforeOfferingCallbackInSecond":          int64(60),
		"NumberOfCallsInQueueBeforeOfferingCallback":      int64(5),
		"CallToAgentRatioThresholdBeforeOfferingCallback": int64(3),
		"CallbackOfferTextToSpeechPrompt":                 "Press 1 for a call back",
		"CallbackOfferAudioFilePromptResourceId":          nil,
		"CallbackEmailNotificationTarget":                 "g1",
		"ServiceLevelThresholdResponseTimeInSecond":       int64(20),
	})

	m.CallbackOfferTextToSpeechPrompt = types.StringNull()
	m.CallbackOfferAudioFileID = types.StringValue("file-cb")
	m.CallbackRequestDtmf = types.StringValue("tone0")
	body, _ = callQueueBody(&m)
	checkCallQueueBody(t, body, map[string]any{
		"CallbackRequestDtmf":                    "Tone0",
		"CallbackOfferTextToSpeechPrompt":        nil,
		"CallbackOfferAudioFilePromptResourceId": "file-cb",
	})
}

func TestCallQueueBodyRedirectPrompts(t *testing.T) {
	m := testCallQueueModel()
	m.OverflowAction = types.StringValue("forward")
	m.OverflowActionTarget = types.StringValue("u1")
	m.OverflowRedirectPersonTextToSpeechPrompt = types.StringValue("Transferring you")
	m.TimeoutAction = types.StringValue("voicemail")
	m.TimeoutActionTarget = types.StringValue("u1")
	m.TimeoutRedirectVoicemailAudioFileID = types.StringValue("file-redirect")
	m.NoAgentAction = types.StringValue("disconnect")
	m.NoAgentDisconnectAudioFileID = types.StringValue("file-disc")
	if diags := validateCallQueue(&m); diags.HasError() {
		t.Fatalf("validateCallQueue: %v", diags)
	}
	body, diags := callQueueBody(&m)
	if diags.HasError() {
		t.Fatalf("callQueueBody diagnostics: %v", diags)
	}
	checkCallQueueBody(t, body, map[string]any{
		"OverflowRedirectPersonTextToSpeechPrompt":      "Transferring you",
		"OverflowRedirectPersonAudioFilePrompt":         nil,
		"OverflowRedirectPhoneNumberTextToSpeechPrompt": nil,
		"TimeoutRedirectVoicemailAudioFilePrompt":       "file-redirect",
		"TimeoutRedirectVoicemailTextToSpeechPrompt":    nil,
		"NoAgentRedirectVoiceAppAudioFilePrompt":        nil,
		"NoAgentDisconnectAudioFilePrompt":              "file-disc",
		"NoAgentDisconnectTextToSpeechPrompt":           nil,
		"OverflowDisconnectAudioFilePrompt":             nil,
	})
}

func TestCallQueueBodyCallPriority(t *testing.T) {
	m := testCallQueueModel()
	body, _ := callQueueBody(&m)
	for _, k := range []string{"OverflowActionCallPriority", "TimeoutActionCallPriority", "NoAgentActionCallPriority"} {
		if got := body[k]; got != int64(3) {
			t.Errorf("body[%s] = %#v; want 3 when null", k, got)
		}
	}
	cur := map[string]any{"Identity": "q1", "NoAgentActionCallPriority": int64(5)}
	updated := callQueueUpdateBody(cur, body, &m, &m)
	if got := updated["NoAgentActionCallPriority"]; got != int64(3) {
		t.Errorf("update NoAgentActionCallPriority = %#v; want 3", got)
	}
	m.NoAgentAction = types.StringValue("forward")
	m.NoAgentActionTarget = types.StringValue("q1")
	m.NoAgentActionCallPriority = types.Int64Value(2)
	body, _ = callQueueBody(&m)
	if got := body["NoAgentActionCallPriority"]; got != int64(2) {
		t.Errorf("NoAgentActionCallPriority = %#v; want 2", got)
	}
	if got := body["OverflowActionCallPriority"]; got != int64(3) {
		t.Errorf("OverflowActionCallPriority = %#v; want 3 when null", got)
	}

	read := testCallQueueModel()
	read.TimeoutActionCallPriority = types.Int64Value(4)
	callQueueModelFromObject(map[string]any{"NoAgentActionCallPriority": float64(1), "OverflowActionCallPriority": nil}, &read)
	if read.NoAgentActionCallPriority.ValueInt64() != 1 {
		t.Errorf("no_agent_action_call_priority = %s; want 1", read.NoAgentActionCallPriority)
	}
	if read.TimeoutActionCallPriority.ValueInt64() != 4 {
		t.Errorf("absent timeout_action_call_priority = %s; want the model value 4", read.TimeoutActionCallPriority)
	}
	if !read.OverflowActionCallPriority.IsNull() {
		t.Errorf("null overflow_action_call_priority = %s; want null", read.OverflowActionCallPriority)
	}
}

func TestCallQueueNoAgentApplyTo(t *testing.T) {
	for _, tc := range []struct {
		val  types.String
		want int
	}{
		{types.StringValue("all_calls"), 0},
		{types.StringValue("new_calls"), 1},
		{types.StringNull(), 0},
	} {
		m := testCallQueueModel()
		m.NoAgentApplyTo = tc.val
		body, diags := callQueueBody(&m)
		if diags.HasError() {
			t.Fatalf("callQueueBody diagnostics: %v", diags)
		}
		if got := body["NoAgentApplyTo"]; got != tc.want {
			t.Errorf("NoAgentApplyTo for %s = %#v; want %d", tc.val, got, tc.want)
		}
	}
	for _, tc := range []struct {
		in   any
		want string
	}{
		{float64(1), "new_calls"},
		{float64(0), "all_calls"},
		{"NewCalls", "new_calls"},
		{nil, "all_calls"},
	} {
		var read callQueueModel
		if diags := callQueueModelFromObject(map[string]any{"NoAgentApplyTo": tc.in}, &read); diags.HasError() {
			t.Fatalf("callQueueModelFromObject diagnostics: %v", diags)
		}
		if got := read.NoAgentApplyTo.ValueString(); got != tc.want {
			t.Errorf("no_agent_apply_to from %#v = %q; want %q", tc.in, got, tc.want)
		}
	}
	read := testCallQueueModel()
	if diags := callQueueModelFromObject(map[string]any{"NoAgentApplyTo": float64(7)}, &read); !diags.HasError() {
		t.Error("expected an error for an unknown NoAgentApplyTo")
	}
}

func TestCallQueueBodyShifts(t *testing.T) {
	m := testCallQueueModel()
	body, _ := callQueueBody(&m)
	checkCallQueueBody(t, body, map[string]any{"ShiftsTeamId": nil, "ShiftsSchedulingGroupId": nil})
	m.ShiftsTeamID = types.StringValue("team-1")
	m.ShiftsSchedulingGroupID = types.StringValue("group-1")
	body, _ = callQueueBody(&m)
	checkCallQueueBody(t, body, map[string]any{"ShiftsTeamId": "team-1", "ShiftsSchedulingGroupId": "group-1"})
}

func TestCallQueueUpdateBodyHiddenAndOboLists(t *testing.T) {
	cur := map[string]any{"Identity": "q1", "HideAuthorizedUsers": []any{"u1"}, "OboResourceAccountIds": []any{"ra1"}}
	cases := []struct {
		name        string
		plan, state func(*callQueueModel)
		want        map[string]any
	}{
		{
			"null in plan and state keeps cur lists",
			func(*callQueueModel) {}, func(*callQueueModel) {},
			map[string]any{"HideAuthorizedUsers": []any{"u1"}, "OboResourceAccountIds": []any{"ra1"}},
		},
		{
			"cleared when the plan unsets them",
			func(*callQueueModel) {},
			func(m *callQueueModel) {
				m.HideAuthorizedUsers, m.OboResourceAccountIDs = testStringList("u1"), testStringList("ra1")
			},
			map[string]any{"HideAuthorizedUsers": []any{}, "OboResourceAccountIds": []any{}},
		},
		{
			"written from the plan",
			func(m *callQueueModel) {
				m.HideAuthorizedUsers, m.OboResourceAccountIDs = testStringList("u2"), testStringList("ra2")
			},
			func(m *callQueueModel) {
				m.HideAuthorizedUsers, m.OboResourceAccountIDs = testStringList("u1"), testStringList("ra1")
			},
			map[string]any{"HideAuthorizedUsers": []string{"u2"}, "OboResourceAccountIds": []string{"ra2"}},
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
			got := callQueueUpdateBody(cur, body, &plan, &state)
			for k, v := range tc.want {
				if !reflect.DeepEqual(got[k], v) {
					t.Errorf("%s = %#v; want %#v", k, got[k], v)
				}
			}
		})
	}
}

func TestCallQueueModelFromObjectNewSettings(t *testing.T) {
	obj := map[string]any{
		"Identity":                                        "q",
		"IsCallbackEnabled":                               true,
		"CallbackRequestDtmf":                             "Tone1",
		"WaitTimeBeforeOfferingCallbackInSecond":          float64(60),
		"NumberOfCallsInQueueBeforeOfferingCallback":      nil,
		"CallToAgentRatioThresholdBeforeOfferingCallback": float64(2),
		"CallbackOfferTextToSpeechPrompt":                 "Press 1",
		"CallbackOfferAudioFilePromptResourceId":          nil,
		"CallbackEmailNotificationTarget":                 map[string]any{"Id": "g1", "Type": "MailBox", "ConfigurationEndpointType": nil},
		"ServiceLevelThresholdResponseTimeInSecond":       float64(30),
		"ShiftsTeamId":                                    "team-1",
		"ShiftsSchedulingGroupId":                         "group-1",
		"HideAuthorizedUsers":                             []any{},
		"OverflowRedirectPersonTextToSpeechPrompt":        "Transferring you",
		"TimeoutRedirectVoicemailAudioFilePrompt":         "file-redirect",
		"NoAgentDisconnectAudioFilePrompt":                "file-disc",
	}
	m := testCallQueueModel()
	if diags := callQueueModelFromObject(obj, &m); diags.HasError() {
		t.Fatalf("callQueueModelFromObject diagnostics: %v", diags)
	}
	strs := []struct {
		name      string
		got, want types.String
	}{
		{"callback_request_dtmf", m.CallbackRequestDtmf, types.StringValue("tone1")},
		{"callback_offer_text_to_speech_prompt", m.CallbackOfferTextToSpeechPrompt, types.StringValue("Press 1")},
		{"callback_offer_audio_file_id", m.CallbackOfferAudioFileID, types.StringNull()},
		{"callback_email_notification_target", m.CallbackEmailNotificationTarget, types.StringValue("g1")},
		{"shifts_team_id", m.ShiftsTeamID, types.StringValue("team-1")},
		{"shifts_scheduling_group_id", m.ShiftsSchedulingGroupID, types.StringValue("group-1")},
		{"overflow_redirect_person_text_to_speech_prompt", m.OverflowRedirectPersonTextToSpeechPrompt, types.StringValue("Transferring you")},
		{"overflow_redirect_person_audio_file_id", m.OverflowRedirectPersonAudioFileID, types.StringNull()},
		{"timeout_redirect_voicemail_audio_file_id", m.TimeoutRedirectVoicemailAudioFileID, types.StringValue("file-redirect")},
		{"no_agent_disconnect_audio_file_id", m.NoAgentDisconnectAudioFileID, types.StringValue("file-disc")},
		{"no_agent_apply_to", m.NoAgentApplyTo, types.StringValue("all_calls")},
	}
	for _, s := range strs {
		if !s.got.Equal(s.want) {
			t.Errorf("%s = %s; want %s", s.name, s.got, s.want)
		}
	}
	if !m.IsCallbackEnabled.ValueBool() {
		t.Error("is_callback_enabled should be true")
	}
	if m.WaitTimeBeforeOfferingCallbackInSecond.ValueInt64() != 60 || !m.NumberOfCallsInQueueBeforeOfferingCallback.IsNull() ||
		m.CallToAgentRatioThresholdBeforeOfferingCallback.ValueInt64() != 2 || m.ServiceLevelThresholdResponseTimeInSecond.ValueInt64() != 30 {
		t.Errorf("ints = %s/%s/%s/%s", m.WaitTimeBeforeOfferingCallbackInSecond, m.NumberOfCallsInQueueBeforeOfferingCallback,
			m.CallToAgentRatioThresholdBeforeOfferingCallback, m.ServiceLevelThresholdResponseTimeInSecond)
	}
	if !m.HideAuthorizedUsers.IsNull() {
		t.Errorf("hide_authorized_users = %s; want null", m.HideAuthorizedUsers)
	}

	var read callQueueModel
	callQueueModelFromObject(map[string]any{"HideAuthorizedUsers": []any{"u1"}, "IsCallbackEnabled": nil}, &read)
	if got := listStrings(read.HideAuthorizedUsers); !reflect.DeepEqual(got, []string{"u1"}) {
		t.Errorf("hide_authorized_users = %v", got)
	}
	if v := read.IsCallbackEnabled; v.IsNull() || v.ValueBool() {
		t.Errorf("null is_callback_enabled read back as %v; want false", v)
	}
}

func TestValidateCallQueueNewSettings(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*callQueueModel)
		wantErr bool
	}{
		{"callback complete", testCallQueueCallback, false},
		{"callback without conference mode", func(m *callQueueModel) {
			testCallQueueCallback(m)
			m.ConferenceMode = types.BoolValue(false)
		}, true},
		{"callback without dtmf", func(m *callQueueModel) {
			testCallQueueCallback(m)
			m.CallbackRequestDtmf = types.StringNull()
		}, true},
		{"callback without prompt", func(m *callQueueModel) {
			testCallQueueCallback(m)
			m.CallbackOfferTextToSpeechPrompt = types.StringNull()
		}, true},
		{"callback with audio prompt", func(m *callQueueModel) {
			testCallQueueCallback(m)
			m.CallbackOfferTextToSpeechPrompt = types.StringNull()
			m.CallbackOfferAudioFileID = types.StringValue("file-cb")
		}, false},
		{"callback with text and audio prompt", func(m *callQueueModel) {
			testCallQueueCallback(m)
			m.CallbackOfferAudioFileID = types.StringValue("file-cb")
		}, true},
		{"callback without threshold", func(m *callQueueModel) {
			testCallQueueCallback(m)
			m.WaitTimeBeforeOfferingCallbackInSecond = types.Int64Null()
		}, true},
		{"callback with ratio threshold only", func(m *callQueueModel) {
			testCallQueueCallback(m)
			m.WaitTimeBeforeOfferingCallbackInSecond = types.Int64Null()
			m.CallToAgentRatioThresholdBeforeOfferingCallback = types.Int64Value(2)
		}, false},
		{"callback with unknown threshold only", func(m *callQueueModel) {
			testCallQueueCallback(m)
			m.WaitTimeBeforeOfferingCallbackInSecond = types.Int64Unknown()
		}, false},
		{"callback unknown enabled skipped", func(m *callQueueModel) {
			m.IsCallbackEnabled = types.BoolUnknown()
			m.CallbackRequestDtmf = types.StringValue("tone1")
		}, false},
		{"callback disabled with dtmf", func(m *callQueueModel) { m.CallbackRequestDtmf = types.StringValue("tone1") }, true},
		{"callback disabled with prompt", func(m *callQueueModel) { m.CallbackOfferAudioFileID = types.StringValue("file-cb") }, true},
		{"callback disabled with threshold", func(m *callQueueModel) { m.NumberOfCallsInQueueBeforeOfferingCallback = types.Int64Value(5) }, true},
		{"callback null enabled with threshold", func(m *callQueueModel) {
			m.IsCallbackEnabled = types.BoolNull()
			m.WaitTimeBeforeOfferingCallbackInSecond = types.Int64Value(60)
		}, true},
		{"redirect person prompt with forward", func(m *callQueueModel) {
			m.OverflowAction = types.StringValue("forward")
			m.OverflowActionTarget = types.StringValue("u1")
			m.OverflowRedirectPersonTextToSpeechPrompt = types.StringValue("x")
		}, false},
		{"redirect person text and audio", func(m *callQueueModel) {
			m.OverflowAction = types.StringValue("forward")
			m.OverflowActionTarget = types.StringValue("u1")
			m.OverflowRedirectPersonTextToSpeechPrompt = types.StringValue("x")
			m.OverflowRedirectPersonAudioFileID = types.StringValue("file-r")
		}, true},
		{"redirect phone number prompt without forward", func(m *callQueueModel) {
			m.OverflowRedirectPhoneNumberTextToSpeechPrompt = types.StringValue("x")
		}, true},
		{"redirect voice app audio without forward", func(m *callQueueModel) {
			m.NoAgentRedirectVoiceAppAudioFileID = types.StringValue("file-r")
		}, true},
		{"redirect voicemail prompt with voicemail", func(m *callQueueModel) {
			m.TimeoutAction = types.StringValue("voicemail")
			m.TimeoutActionTarget = types.StringValue("u1")
			m.TimeoutRedirectVoicemailAudioFileID = types.StringValue("file-r")
		}, false},
		{"redirect voicemail prompt with forward", func(m *callQueueModel) {
			m.TimeoutAction = types.StringValue("forward")
			m.TimeoutActionTarget = types.StringValue("u1")
			m.TimeoutRedirectVoicemailTextToSpeechPrompt = types.StringValue("x")
		}, true},
		{"disconnect audio", func(m *callQueueModel) { m.TimeoutDisconnectAudioFileID = types.StringValue("file-d") }, false},
		{"disconnect text and audio", func(m *callQueueModel) {
			m.TimeoutDisconnectTextToSpeechPrompt = types.StringValue("Bye")
			m.TimeoutDisconnectAudioFileID = types.StringValue("file-d")
		}, true},
		{"call priority with forward", func(m *callQueueModel) {
			m.NoAgentAction = types.StringValue("forward")
			m.NoAgentActionTarget = types.StringValue("q1")
			m.NoAgentActionCallPriority = types.Int64Value(1)
		}, false},
		{"call priority without forward", func(m *callQueueModel) { m.OverflowActionCallPriority = types.Int64Value(3) }, true},
		{"unknown call priority skipped", func(m *callQueueModel) { m.OverflowActionCallPriority = types.Int64Unknown() }, false},
		{"shifts complete", func(m *callQueueModel) {
			m.ShiftsTeamID = types.StringValue("team-1")
			m.ShiftsSchedulingGroupID = types.StringValue("group-1")
		}, false},
		{"shifts team only", func(m *callQueueModel) { m.ShiftsTeamID = types.StringValue("team-1") }, true},
		{"shifts scheduling group only", func(m *callQueueModel) { m.ShiftsSchedulingGroupID = types.StringValue("group-1") }, true},
		{"shifts with users", func(m *callQueueModel) {
			m.ShiftsTeamID = types.StringValue("team-1")
			m.ShiftsSchedulingGroupID = types.StringValue("group-1")
			m.Users = testStringList("u1")
		}, true},
		{"shifts with distribution lists", func(m *callQueueModel) {
			m.ShiftsTeamID = types.StringValue("team-1")
			m.ShiftsSchedulingGroupID = types.StringValue("group-1")
			m.DistributionLists = testStringList("g1")
		}, true},
		{"shifts with channel", func(m *callQueueModel) {
			m.ShiftsTeamID = types.StringValue("team-1")
			m.ShiftsSchedulingGroupID = types.StringValue("group-1")
			m.ChannelID = types.StringValue("19:abc@thread.tacv2")
			m.ChannelUserObjectID = types.StringValue("owner-1")
		}, true},
		{"unknown channel with owner", func(m *callQueueModel) {
			m.ChannelID = types.StringUnknown()
			m.ChannelUserObjectID = types.StringValue("owner-1")
		}, false},
		{"hidden authorized users", func(m *callQueueModel) {
			m.AuthorizedUsers = testStringList("u1")
			m.HideAuthorizedUsers = testStringList("u1")
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
