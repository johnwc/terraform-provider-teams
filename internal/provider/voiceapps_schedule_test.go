package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
)

func testRange(start, end string) scheduleRangeModel {
	return scheduleRangeModel{Start: types.StringValue(start), End: types.StringValue(end)}
}

// testScheduleWeekly returns a weekly schedule model.
func testScheduleWeekly(t *testing.T, w scheduleWeeklyModel) scheduleModel {
	t.Helper()
	v, d := types.ObjectValueFrom(context.Background(), scheduleWeeklyAttrTypes, w)
	if d.HasError() {
		t.Fatalf("ObjectValueFrom diagnostics: %v", d)
	}
	return scheduleModel{
		ID:                         types.StringUnknown(),
		Name:                       types.StringValue("tf business hours"),
		Weekly:                     v,
		Fixed:                      types.ObjectNull(scheduleFixedAttrTypes),
		AssociatedConfigurationIDs: types.ListUnknown(types.StringType),
	}
}

// testScheduleFixed returns a fixed schedule model.
func testScheduleFixed(t *testing.T, ranges ...scheduleRangeModel) scheduleModel {
	t.Helper()
	v, d := types.ObjectValueFrom(context.Background(), scheduleFixedAttrTypes, scheduleFixedModel{DateRange: ranges})
	if d.HasError() {
		t.Fatalf("ObjectValueFrom diagnostics: %v", d)
	}
	return scheduleModel{
		ID:                         types.StringUnknown(),
		Name:                       types.StringValue("tf holidays"),
		Weekly:                     types.ObjectNull(scheduleWeeklyAttrTypes),
		Fixed:                      v,
		AssociatedConfigurationIDs: types.ListUnknown(types.StringType),
	}
}

func TestScheduleBodyWeekly(t *testing.T) {
	m := testScheduleWeekly(t, scheduleWeeklyModel{
		Complement: types.BoolNull(),
		Monday:     []scheduleRangeModel{testRange("08:00", "17:00")},
		Friday:     []scheduleRangeModel{testRange("08:00:00", "12:00"), testRange("13:00", "16:30:15")},
	})
	body, diags := scheduleBody(context.Background(), &m)
	if diags.HasError() {
		t.Fatalf("scheduleBody diagnostics: %v", diags)
	}
	want := map[string]any{
		"Name": "tf business hours",
		"Type": "WeeklyRecurrence",
		"WeeklyRecurrentSchedule": map[string]any{
			"IsComplemented":  false,
			"RecurrenceRange": map[string]any{"Type": "NoEndDate", "Start": "0001-01-01T00:00:00+00:00"},
			"MondayHours":     []any{map[string]any{"Start": "08:00:00", "End": "17:00:00"}},
			"TuesdayHours":    []any{},
			"WednesdayHours":  []any{},
			"ThursdayHours":   []any{},
			"FridayHours": []any{
				map[string]any{"Start": "08:00:00", "End": "12:00:00"},
				map[string]any{"Start": "13:00:00", "End": "16:30:15"},
			},
			"SaturdayHours": []any{},
			"SundayHours":   []any{},
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v\nwant %#v", body, want)
	}
}

func TestScheduleBodyWeeklyComplement(t *testing.T) {
	m := testScheduleWeekly(t, scheduleWeeklyModel{Complement: types.BoolValue(true)})
	body, diags := scheduleBody(context.Background(), &m)
	if diags.HasError() {
		t.Fatalf("scheduleBody diagnostics: %v", diags)
	}
	ws := body["WeeklyRecurrentSchedule"].(map[string]any)
	if ws["IsComplemented"] != true {
		t.Errorf("IsComplemented = %#v; want true", ws["IsComplemented"])
	}
}

func TestScheduleBodyFixed(t *testing.T) {
	m := testScheduleFixed(t, testRange("2026-12-25T00:00:00", "2026-12-26T00:00:00"), testRange("2027-01-01T00:00:00", "2027-01-02T00:00:00"))
	body, diags := scheduleBody(context.Background(), &m)
	if diags.HasError() {
		t.Fatalf("scheduleBody diagnostics: %v", diags)
	}
	want := map[string]any{
		"Name": "tf holidays",
		"Type": "Fixed",
		"FixedSchedule": map[string]any{"DateTimeRanges": []any{
			map[string]any{"Start": "2026-12-25T00:00:00", "End": "2026-12-26T00:00:00"},
			map[string]any{"Start": "2027-01-01T00:00:00", "End": "2027-01-02T00:00:00"},
		}},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v\nwant %#v", body, want)
	}
}

func TestScheduleModelFromObjectWeekly(t *testing.T) {
	ctx := context.Background()
	m := testScheduleWeekly(t, scheduleWeeklyModel{
		Complement: types.BoolNull(),
		Monday:     []scheduleRangeModel{testRange("08:00", "17:00")},
		Tuesday:    []scheduleRangeModel{testRange("08:00", "17:00")},
		Sunday:     []scheduleRangeModel{},
	})
	obj := map[string]any{
		"Id":   "sched-1",
		"Name": "tf business hours",
		"Type": "WeeklyRecurrence",
		"WeeklyRecurrentSchedule": map[string]any{
			"IsComplemented":  false,
			"MondayHours":     []any{map[string]any{"Start": "08:00:00", "End": "17:00:00"}},
			"TuesdayHours":    []any{map[string]any{"Start": "09:00:00", "End": "17:00:00"}},
			"WednesdayHours":  []any{},
			"ThursdayHours":   []any{},
			"FridayHours":     []any{},
			"SaturdayHours":   []any{},
			"SundayHours":     []any{},
			"RecurrenceRange": map[string]any{"Type": "NoEndDate", "Start": "0001-01-01T00:00:00+00:00", "End": nil, "NumberOfOccurrences": float64(0)},
		},
		"FixedSchedule":              nil,
		"AssociatedConfigurationIds": []any{"aa-1"},
	}
	if diags := scheduleModelFromObject(ctx, obj, &m); diags.HasError() {
		t.Fatalf("scheduleModelFromObject diagnostics: %v", diags)
	}
	if m.ID.ValueString() != "sched-1" {
		t.Errorf("id = %s", m.ID)
	}
	if !m.Fixed.IsNull() {
		t.Errorf("fixed = %s; want null", m.Fixed)
	}
	w, d := m.weekly(ctx)
	if d.HasError() || w == nil {
		t.Fatalf("weekly = %v, %v", w, d)
	}
	if !w.Complement.IsNull() {
		t.Errorf("complement = %s; want null", w.Complement)
	}
	if want := []scheduleRangeModel{testRange("08:00", "17:00")}; !reflect.DeepEqual(w.Monday, want) {
		t.Errorf("monday = %v; want %v", w.Monday, want)
	}
	if want := []scheduleRangeModel{testRange("09:00:00", "17:00")}; !reflect.DeepEqual(w.Tuesday, want) {
		t.Errorf("tuesday = %v; want %v", w.Tuesday, want)
	}
	if w.Wednesday != nil {
		t.Errorf("wednesday = %v; want null", w.Wednesday)
	}
	if w.Sunday == nil || len(w.Sunday) != 0 {
		t.Errorf("sunday = %#v; want empty list", w.Sunday)
	}
	if got := listStrings(m.AssociatedConfigurationIDs); !reflect.DeepEqual(got, []string{"aa-1"}) {
		t.Errorf("associated_configuration_ids = %v", got)
	}
}

func TestScheduleModelFromObjectImport(t *testing.T) {
	ctx := context.Background()
	m := scheduleModel{
		ID:                         types.StringValue("sched-1"),
		Weekly:                     types.ObjectNull(scheduleWeeklyAttrTypes),
		Fixed:                      types.ObjectNull(scheduleFixedAttrTypes),
		AssociatedConfigurationIDs: types.ListNull(types.StringType),
	}
	obj := map[string]any{
		"Id":   "sched-1",
		"Name": "tf holidays",
		"Type": "Fixed",
		"FixedSchedule": map[string]any{"DateTimeRanges": []any{
			map[string]any{"Start": "2026-12-25T00:00:00", "End": "2026-12-26T00:00:00"},
		}},
		"WeeklyRecurrentSchedule":    nil,
		"AssociatedConfigurationIds": []any{},
	}
	if diags := scheduleModelFromObject(ctx, obj, &m); diags.HasError() {
		t.Fatalf("scheduleModelFromObject diagnostics: %v", diags)
	}
	if !m.Weekly.IsNull() {
		t.Errorf("weekly = %s; want null", m.Weekly)
	}
	f, d := m.fixed(ctx)
	if d.HasError() || f == nil {
		t.Fatalf("fixed = %v, %v", f, d)
	}
	if want := []scheduleRangeModel{testRange("2026-12-25T00:00:00", "2026-12-26T00:00:00")}; !reflect.DeepEqual(f.DateRange, want) {
		t.Errorf("date_range = %v; want %v", f.DateRange, want)
	}
	if m.Name.ValueString() != "tf holidays" {
		t.Errorf("name = %s", m.Name)
	}
}

func TestScheduleModelFromObjectComplement(t *testing.T) {
	ctx := context.Background()
	m := testScheduleWeekly(t, scheduleWeeklyModel{Complement: types.BoolValue(false)})
	obj := map[string]any{"Id": "s", "Name": "n", "Type": "WeeklyRecurrence", "WeeklyRecurrentSchedule": map[string]any{"IsComplemented": false}}
	if diags := scheduleModelFromObject(ctx, obj, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	w, _ := m.weekly(ctx)
	if !w.Complement.Equal(types.BoolValue(false)) {
		t.Errorf("complement = %s; want false", w.Complement)
	}
	obj["WeeklyRecurrentSchedule"] = map[string]any{"IsComplemented": true}
	m = testScheduleWeekly(t, scheduleWeeklyModel{Complement: types.BoolNull()})
	if diags := scheduleModelFromObject(ctx, obj, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	w, _ = m.weekly(ctx)
	if !w.Complement.Equal(types.BoolValue(true)) {
		t.Errorf("complement = %s; want true", w.Complement)
	}
}

func TestScheduleModelFromObjectUnknownType(t *testing.T) {
	m := testScheduleWeekly(t, scheduleWeeklyModel{})
	diags := scheduleModelFromObject(context.Background(), map[string]any{"Id": "s", "Type": "Recurring"}, &m)
	if !diags.HasError() {
		t.Fatal("expected an error for an unknown Type")
	}
	if got := diags.Errors()[0].Summary(); got != "Unexpected Type value" {
		t.Errorf("summary = %q", got)
	}
}

func TestClockValue(t *testing.T) {
	cases := []struct {
		name  string
		wire  string
		prior types.String
		want  types.String
	}{
		{"short form kept", "08:00:00", types.StringValue("08:00"), types.StringValue("08:00")},
		{"long form kept", "08:00:00", types.StringValue("08:00:00"), types.StringValue("08:00:00")},
		{"changed", "08:30:00", types.StringValue("08:00"), types.StringValue("08:30:00")},
		{"no prior", "08:00:00", types.StringNull(), types.StringValue("08:00:00")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clockValue(tc.wire, tc.prior); !got.Equal(tc.want) {
				t.Errorf("clockValue = %s; want %s", got, tc.want)
			}
		})
	}
}

func TestDateValue(t *testing.T) {
	cases := []struct {
		name  string
		wire  string
		prior types.String
		want  types.String
	}{
		{"same", "2026-12-25T00:00:00", types.StringValue("2026-12-25T00:00:00"), types.StringValue("2026-12-25T00:00:00")},
		{"zone suffix", "2026-12-25T00:00:00Z", types.StringValue("2026-12-25T00:00:00"), types.StringValue("2026-12-25T00:00:00")},
		{"no prior with zone", "2026-12-25T00:00:00+00:00", types.StringNull(), types.StringValue("2026-12-25T00:00:00")},
		{"changed", "2026-12-24T00:00:00", types.StringValue("2026-12-25T00:00:00"), types.StringValue("2026-12-24T00:00:00")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dateValue(tc.wire, tc.prior); !got.Equal(tc.want) {
				t.Errorf("dateValue = %s; want %s", got, tc.want)
			}
		})
	}
}

func TestParseClock(t *testing.T) {
	good := map[string]int64{"00:00": 0, "08:00": 28800, "08:00:00": 28800, "23:59:59": 86399, "17:30": 63000}
	for in, want := range good {
		if got, ok := parseClock(in); !ok || got != want {
			t.Errorf("parseClock(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "8:00", "24:01", "25:00", "08:60", "08:00:60", "08", "08:00:00:00", "ab:cd"} {
		if _, ok := parseClock(in); ok {
			t.Errorf("parseClock(%q) should fail", in)
		}
	}
	if got := clockWire("08:00"); got != "08:00:00" {
		t.Errorf("clockWire(08:00) = %q", got)
	}
}

func TestValidateSchedule(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		model   func(*testing.T) scheduleModel
		wantErr bool
	}{
		{"weekly ok", func(t *testing.T) scheduleModel {
			return testScheduleWeekly(t, scheduleWeeklyModel{Monday: []scheduleRangeModel{testRange("08:00", "17:00:00")}})
		}, false},
		{"fixed ok", func(t *testing.T) scheduleModel {
			return testScheduleFixed(t, testRange("2026-12-25T00:00:00", "2026-12-26T00:00:00"))
		}, false},
		{"neither", func(t *testing.T) scheduleModel {
			m := testScheduleFixed(t)
			m.Fixed = types.ObjectNull(scheduleFixedAttrTypes)
			return m
		}, true},
		{"both", func(t *testing.T) scheduleModel {
			m := testScheduleWeekly(t, scheduleWeeklyModel{})
			m.Fixed = testScheduleFixed(t, testRange("2026-12-25T00:00:00", "2026-12-26T00:00:00")).Fixed
			return m
		}, true},
		{"end before start", func(t *testing.T) scheduleModel {
			return testScheduleWeekly(t, scheduleWeeklyModel{Monday: []scheduleRangeModel{testRange("17:00", "08:00")}})
		}, true},
		{"end equals start", func(t *testing.T) scheduleModel {
			return testScheduleWeekly(t, scheduleWeeklyModel{Monday: []scheduleRangeModel{testRange("08:00", "08:00:00")}})
		}, true},
		{"bad time", func(t *testing.T) scheduleModel {
			return testScheduleWeekly(t, scheduleWeeklyModel{Monday: []scheduleRangeModel{testRange("8am", "17:00")}})
		}, true},
		{"fixed end before start", func(t *testing.T) scheduleModel {
			return testScheduleFixed(t, testRange("2026-12-26T00:00:00", "2026-12-25T00:00:00"))
		}, true},
		{"fixed with zone", func(t *testing.T) scheduleModel {
			return testScheduleFixed(t, testRange("2026-12-25T00:00:00Z", "2026-12-26T00:00:00"))
		}, true},
		{"unknown value skipped", func(t *testing.T) scheduleModel {
			return testScheduleWeekly(t, scheduleWeeklyModel{Monday: []scheduleRangeModel{{Start: types.StringUnknown(), End: types.StringValue("08:00")}}})
		}, false},
		{"weekly conversion error", func(t *testing.T) scheduleModel {
			m := testScheduleWeekly(t, scheduleWeeklyModel{})
			m.Weekly = types.ObjectValueMust(map[string]attr.Type{"complement": types.StringType}, map[string]attr.Value{"complement": types.StringValue("x")})
			return m
		}, true},
		{"fixed conversion error", func(t *testing.T) scheduleModel {
			m := testScheduleFixed(t)
			m.Fixed = types.ObjectValueMust(map[string]attr.Type{"date_range": types.StringType}, map[string]attr.Value{"date_range": types.StringValue("x")})
			return m
		}, true},
		{"weekly unknown day skipped", func(t *testing.T) scheduleModel {
			m := testScheduleWeekly(t, scheduleWeeklyModel{})
			attrs := map[string]attr.Value{"complement": types.BoolNull()}
			for name := range scheduleWeeklyAttrTypes {
				if name != "complement" {
					attrs[name] = types.ListNull(scheduleRangeType)
				}
			}
			attrs["monday"] = types.ListUnknown(scheduleRangeType)
			m.Weekly = types.ObjectValueMust(scheduleWeeklyAttrTypes, attrs)
			return m
		}, false},
		{"fixed unknown date_range skipped", func(t *testing.T) scheduleModel {
			m := testScheduleFixed(t)
			m.Fixed = types.ObjectValueMust(scheduleFixedAttrTypes, map[string]attr.Value{"date_range": types.ListUnknown(scheduleRangeType)})
			return m
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model(t)
			if got := validateSchedule(ctx, &m).HasError(); got != tc.wantErr {
				t.Errorf("HasError = %v; want %v", got, tc.wantErr)
			}
		})
	}
}

func TestUnwrapSchedule(t *testing.T) {
	inner := map[string]any{"Id": "s1", "Name": "n"}
	for name, in := range map[string][]map[string]any{
		"wrapped": {{"Schedule": inner}},
		"bare":    {inner},
	} {
		if got := scheduleKind.unwrap(in); !reflect.DeepEqual(got, inner) {
			t.Errorf("%s: unwrap = %#v", name, got)
		}
	}
	if got := scheduleKind.unwrap(nil); got != nil {
		t.Errorf("empty: unwrap = %#v", got)
	}
}

func TestIsScheduleGone(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404", &teamsapi.APIError{Status: 404, Message: "Not Found"}, true},
		{"400 not found", &teamsapi.APIError{Status: 400, Message: "Schedule not found."}, true},
		{"400 invalid identity", &teamsapi.APIError{Status: 400, Message: "Invalid identity (x)"}, true},
		{"other 400", &teamsapi.APIError{Status: 400, Message: "Schedule is in use"}, false},
		{"500 not found", &teamsapi.APIError{Status: 500, Message: "not found"}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isScheduleGone(tc.err); got != tc.want {
				t.Errorf("isScheduleGone = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestVoiceAppOps(t *testing.T) {
	cases := []struct {
		kind       voiceAppKind
		cmdlet     string
		collection string
		item       string
	}{
		{callQueueKind, "Get-CsCallQueue", "/Teams.VoiceApps/callqueues", "/Teams.VoiceApps/callqueues/{identity}"},
		{scheduleKind, "Get-CsOnlineSchedule", "/Teams.VoiceApps/schedules", "/Teams.VoiceApps/schedules/{identity}"},
		{autoAttendantKind, "Get-CsAutoAttendant", "/Teams.VoiceApps/auto-attendants", "/Teams.VoiceApps/auto-attendants/{identity}"},
	}
	for _, tc := range cases {
		op := tc.kind.op("Get", "GET", tc.kind.itemPath())
		if op.CmdletName != tc.cmdlet || op.Path != tc.item || op.Transport != teamsapi.ConfigAPI || op.Kind != teamsapi.AutoRest {
			t.Errorf("op = %#v", op)
		}
		if tc.kind.path != tc.collection {
			t.Errorf("path = %q; want %q", tc.kind.path, tc.collection)
		}
	}
}

func TestScheduleMidnightEnd(t *testing.T) {
	for _, in := range []string{"24:00", "24:00:00", "1.00:00:00"} {
		if got, ok := parseClock(in); !ok || got != 24*3600 {
			t.Errorf("parseClock(%q) = %d, %v; want 86400, true", in, got, ok)
		}
	}
	if got := clockWire("24:00"); got != "1.00:00:00" {
		t.Errorf("clockWire(24:00) = %q; want 1.00:00:00", got)
	}
	if got := clockValue("1.00:00:00", types.StringNull()); got.ValueString() != "24:00" {
		t.Errorf("clockValue(1.00:00:00, null) = %q; want 24:00", got.ValueString())
	}
	if got := clockValue("1.00:00:00", types.StringValue("24:00:00")); got.ValueString() != "24:00:00" {
		t.Errorf("clockValue kept %q; want prior 24:00:00", got.ValueString())
	}
	if _, ok := parseClock("25:00"); ok {
		t.Error("parseClock(25:00) accepted")
	}
}
