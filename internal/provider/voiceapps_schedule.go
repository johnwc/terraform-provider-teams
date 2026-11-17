package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/terraprovider/terraform-provider-teams/internal/clients"
)

var (
	_ resource.Resource                   = &scheduleResource{}
	_ resource.ResourceWithConfigure      = &scheduleResource{}
	_ resource.ResourceWithImportState    = &scheduleResource{}
	_ resource.ResourceWithValidateConfig = &scheduleResource{}
)

type scheduleResource struct{ client *clients.Client }

// NewScheduleResource manages a Teams auto attendant schedule (New-CsOnlineSchedule / Get-CsOnlineSchedule / Set-CsOnlineSchedule / Remove-CsOnlineSchedule).
func NewScheduleResource() resource.Resource { return &scheduleResource{} }

const (
	scheduleTypeWeekly = "WeeklyRecurrence"
	scheduleTypeFixed  = "Fixed"
	scheduleDateLayout = "2006-01-02T15:04:05"
)

type scheduleModel struct {
	ID                         types.String `tfsdk:"id"`
	Name                       types.String `tfsdk:"name"`
	Weekly                     types.Object `tfsdk:"weekly"`
	Fixed                      types.Object `tfsdk:"fixed"`
	AssociatedConfigurationIDs types.List   `tfsdk:"associated_configuration_ids"`
}

type scheduleRangeModel struct {
	Start types.String `tfsdk:"start"`
	End   types.String `tfsdk:"end"`
}

type scheduleWeeklyModel struct {
	Complement types.Bool           `tfsdk:"complement"`
	Monday     []scheduleRangeModel `tfsdk:"monday"`
	Tuesday    []scheduleRangeModel `tfsdk:"tuesday"`
	Wednesday  []scheduleRangeModel `tfsdk:"wednesday"`
	Thursday   []scheduleRangeModel `tfsdk:"thursday"`
	Friday     []scheduleRangeModel `tfsdk:"friday"`
	Saturday   []scheduleRangeModel `tfsdk:"saturday"`
	Sunday     []scheduleRangeModel `tfsdk:"sunday"`
}

type scheduleFixedModel struct {
	DateRange []scheduleRangeModel `tfsdk:"date_range"`
}

// scheduleDay pairs a weekday attribute with its Teams field.
type scheduleDay struct {
	attr, wire string
	ranges     *[]scheduleRangeModel
}

func (w *scheduleWeeklyModel) days() []scheduleDay {
	return []scheduleDay{
		{"monday", "MondayHours", &w.Monday},
		{"tuesday", "TuesdayHours", &w.Tuesday},
		{"wednesday", "WednesdayHours", &w.Wednesday},
		{"thursday", "ThursdayHours", &w.Thursday},
		{"friday", "FridayHours", &w.Friday},
		{"saturday", "SaturdayHours", &w.Saturday},
		{"sunday", "SundayHours", &w.Sunday},
	}
}

var (
	scheduleRangeType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"start": types.StringType,
		"end":   types.StringType,
	}}
	scheduleWeeklyAttrTypes = map[string]attr.Type{
		"complement": types.BoolType,
		"monday":     types.ListType{ElemType: scheduleRangeType},
		"tuesday":    types.ListType{ElemType: scheduleRangeType},
		"wednesday":  types.ListType{ElemType: scheduleRangeType},
		"thursday":   types.ListType{ElemType: scheduleRangeType},
		"friday":     types.ListType{ElemType: scheduleRangeType},
		"saturday":   types.ListType{ElemType: scheduleRangeType},
		"sunday":     types.ListType{ElemType: scheduleRangeType},
	}
	scheduleFixedAttrTypes = map[string]attr.Type{
		"date_range": types.ListType{ElemType: scheduleRangeType},
	}
)

// weekly returns the weekly block, or nil when it is null or unknown.
func (m *scheduleModel) weekly(ctx context.Context) (*scheduleWeeklyModel, diag.Diagnostics) {
	if m.Weekly.IsNull() || m.Weekly.IsUnknown() {
		return nil, nil
	}
	var w scheduleWeeklyModel
	diags := m.Weekly.As(ctx, &w, basetypes.ObjectAsOptions{UnhandledUnknownAsEmpty: true})
	return &w, diags
}

// fixed returns the fixed block, or nil when it is null or unknown.
func (m *scheduleModel) fixed(ctx context.Context) (*scheduleFixedModel, diag.Diagnostics) {
	if m.Fixed.IsNull() || m.Fixed.IsUnknown() {
		return nil, nil
	}
	var f scheduleFixedModel
	diags := m.Fixed.As(ctx, &f, basetypes.ObjectAsOptions{UnhandledUnknownAsEmpty: true})
	return &f, diags
}

func (r *scheduleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule"
}

func scheduleRangeObject(startDesc, endDesc string) schema.NestedAttributeObject {
	return schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
		"start": schema.StringAttribute{Required: true, Description: startDesc},
		"end":   schema.StringAttribute{Required: true, Description: endDesc},
	}}
}

// scheduleKindReplace requires replacement when the block switches between set and unset.
func scheduleKindReplace() planmodifier.Object {
	return objectplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.ObjectRequest, resp *objectplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = req.StateValue.IsNull() != req.PlanValue.IsNull()
		},
		"Changing between weekly and fixed requires replacement.",
		"Changing between weekly and fixed requires replacement.",
	)
}

func (r *scheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	hours := scheduleRangeObject("Start time, HH:MM or HH:MM:SS.", "End time, HH:MM or HH:MM:SS; 24:00 is the end of the day. Must be after start.")
	weekly := map[string]schema.Attribute{
		"complement": schema.BoolAttribute{Optional: true, Description: "Apply the schedule outside the listed hours instead of during them. Defaults to false."},
	}
	for _, d := range (&scheduleWeeklyModel{}).days() {
		weekly[d.attr] = schema.ListNestedAttribute{Optional: true, NestedObject: hours, Description: fmt.Sprintf("Time ranges on %s.", strings.ToUpper(d.attr[:1])+d.attr[1:])}
	}
	resp.Schema = schema.Schema{
		Description: "Manages a Teams auto attendant schedule via New-CsOnlineSchedule / Get-CsOnlineSchedule / Set-CsOnlineSchedule / Remove-CsOnlineSchedule. Exactly one of weekly or fixed must be set.",
		Attributes: map[string]schema.Attribute{
			"id":                           schema.StringAttribute{Computed: true, Description: "Schedule id.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":                         schema.StringAttribute{Required: true, Description: "Display name of the schedule."},
			"associated_configuration_ids": schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Ids of the auto attendants using the schedule.", PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()}},
		},
		Blocks: map[string]schema.Block{
			"weekly": schema.SingleNestedBlock{
				Description:   "Weekly recurring hours. Changing to fixed requires replacement.",
				Attributes:    weekly,
				PlanModifiers: []planmodifier.Object{scheduleKindReplace()},
			},
			"fixed": schema.SingleNestedBlock{
				Description: "Fixed date ranges, e.g. holidays. Changing to weekly requires replacement.",
				Attributes: map[string]schema.Attribute{
					"date_range": schema.ListNestedAttribute{
						Optional:     true,
						Description:  "Date ranges. Required when the fixed block is set.",
						NestedObject: scheduleRangeObject("Start, local date and time without zone, e.g. 2026-12-25T00:00:00.", "End, local date and time without zone. Must be after start."),
					},
				},
				PlanModifiers: []planmodifier.Object{scheduleKindReplace()},
			},
		},
	}
}

func (r *scheduleResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*clients.Client)
}

func (r *scheduleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg scheduleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateSchedule(ctx, &cfg)...)
}

func (r *scheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, d := scheduleBody(ctx, &plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := createVoiceApp(ctx, r.client.API, scheduleKind, body)
	if err != nil {
		resp.Diagnostics.AddError(scheduleKind.cmdlet("New")+" failed", err.Error())
		return
	}
	id := voiceAppID(created)
	if id == "" {
		resp.Diagnostics.AddError(scheduleKind.cmdlet("New")+" failed", "the response did not include the schedule id")
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, scheduleKind, id)
	if err != nil {
		resp.Diagnostics.AddError(scheduleKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(scheduleModelFromObject(ctx, obj, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, scheduleKind, state.ID.ValueString())
	if err != nil {
		if isScheduleGone(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(scheduleKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	resp.Diagnostics.Append(scheduleModelFromObject(ctx, obj, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state scheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	body, d := scheduleBody(ctx, &plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	cur, err := getVoiceApp(ctx, r.client.API, scheduleKind, id)
	if err != nil {
		resp.Diagnostics.AddError(scheduleKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	if err := putVoiceApp(ctx, r.client.API, scheduleKind, id, overlayObject(cur, body)); err != nil {
		resp.Diagnostics.AddError(scheduleKind.cmdlet("Set")+" failed", err.Error())
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, scheduleKind, id)
	if err != nil {
		resp.Diagnostics.AddError(scheduleKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(scheduleModelFromObject(ctx, obj, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteVoiceApp(ctx, r.client.API, scheduleKind, state.ID.ValueString()); err != nil && !isScheduleGone(err) {
		resp.Diagnostics.AddError(scheduleKind.cmdlet("Remove")+" failed", err.Error())
	}
}

func (r *scheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// validateSchedule checks that exactly one of weekly or fixed is set and that
// every range is well formed with start before end. Values that are not yet
// known are skipped.
func validateSchedule(ctx context.Context, m *scheduleModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if m.Weekly.IsUnknown() || m.Fixed.IsUnknown() {
		return diags
	}
	switch {
	case m.Weekly.IsNull() && m.Fixed.IsNull():
		diags.AddError("Invalid schedule configuration", "Exactly one of the weekly or fixed blocks must be set.")
		return diags
	case !m.Weekly.IsNull() && !m.Fixed.IsNull():
		diags.AddError("Invalid schedule configuration", "Only one of the weekly or fixed blocks may be set.")
		return diags
	}
	w, d := m.weekly(ctx)
	diags.Append(d...)
	if w != nil && !d.HasError() {
		for _, day := range w.days() {
			for i, rg := range *day.ranges {
				p := path.Root("weekly").AtName(day.attr).AtListIndex(i)
				diags.Append(validateScheduleRange(p, rg, parseClock, "HH:MM or HH:MM:SS")...)
			}
		}
	}
	f, d := m.fixed(ctx)
	diags.Append(d...)
	if f != nil && !d.HasError() {
		if len(f.DateRange) == 0 && !m.Fixed.Attributes()["date_range"].IsUnknown() {
			diags.AddAttributeError(path.Root("fixed").AtName("date_range"), "Invalid schedule configuration", "The fixed block needs at least one date_range.")
		}
		for i, rg := range f.DateRange {
			p := path.Root("fixed").AtName("date_range").AtListIndex(i)
			diags.Append(validateScheduleRange(p, rg, parseScheduleDate, "YYYY-MM-DDTHH:MM:SS without a zone")...)
		}
	}
	return diags
}

func validateScheduleRange(p path.Path, rg scheduleRangeModel, parse func(string) (int64, bool), format string) diag.Diagnostics {
	var diags diag.Diagnostics
	if rg.Start.IsUnknown() || rg.End.IsUnknown() || rg.Start.IsNull() || rg.End.IsNull() {
		return diags
	}
	start, okS := parse(rg.Start.ValueString())
	if !okS {
		diags.AddAttributeError(p.AtName("start"), "Invalid schedule configuration", fmt.Sprintf("%q is not valid; use %s.", rg.Start.ValueString(), format))
	}
	end, okE := parse(rg.End.ValueString())
	if !okE {
		diags.AddAttributeError(p.AtName("end"), "Invalid schedule configuration", fmt.Sprintf("%q is not valid; use %s.", rg.End.ValueString(), format))
	}
	if okS && okE && start >= end {
		diags.AddAttributeError(p.AtName("end"), "Invalid schedule configuration",
			fmt.Sprintf("end %q must be after start %q.", rg.End.ValueString(), rg.Start.ValueString()))
	}
	return diags
}

// midnightEnd is the end of day, written "24:00" in config and "1.00:00:00" on the wire.
const (
	midnightEnd       = 24 * 3600
	midnightEndWire   = "1.00:00:00"
	midnightEndConfig = "24:00"
)

// parseClock parses "HH:MM" or "HH:MM:SS" into seconds after midnight; "24:00", "24:00:00" and "1.00:00:00" are the end of day.
func parseClock(s string) (int64, bool) {
	switch s {
	case midnightEndConfig, "24:00:00", midnightEndWire:
		return midnightEnd, true
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, false
	}
	limits := []int64{23, 59, 59}
	var total int64
	for i, p := range parts {
		if len(p) != 2 {
			return 0, false
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n < 0 || n > limits[i] {
			return 0, false
		}
		total = total*60 + n
	}
	if len(parts) == 2 {
		total *= 60
	}
	return total, true
}

// clockWire returns the "HH:MM:SS" form of a configured time.
func clockWire(s string) string {
	secs, ok := parseClock(s)
	if !ok {
		return s
	}
	if secs == midnightEnd {
		return midnightEndWire
	}
	return fmt.Sprintf("%02d:%02d:%02d", secs/3600, secs/60%60, secs%60)
}

// clockValue maps a read-back time, keeping the prior value when it denotes the same time.
func clockValue(wire string, prior types.String) types.String {
	if !prior.IsNull() && !prior.IsUnknown() {
		p, okP := parseClock(prior.ValueString())
		w, okW := parseClock(wire)
		if okP && okW && p == w {
			return prior
		}
	}
	if wire == midnightEndWire {
		return types.StringValue(midnightEndConfig)
	}
	return types.StringValue(wire)
}

// parseScheduleDate parses a configured date and time without a zone into Unix seconds.
func parseScheduleDate(s string) (int64, bool) {
	t, err := time.Parse(scheduleDateLayout, s)
	if err != nil {
		return 0, false
	}
	return t.Unix(), true
}

// parseWireDate parses a read-back date and time, with or without a zone, and
// returns its wall-clock value.
func parseWireDate(s string) (time.Time, bool) {
	for _, layout := range []string{scheduleDateLayout, "2006-01-02T15:04:05.9999999", time.RFC3339Nano, "2006-01-02T15:04:05.9999999Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC), true
		}
	}
	return time.Time{}, false
}

// dateValue maps a read-back date and time, keeping the prior value when it
// denotes the same wall-clock time.
func dateValue(wire string, prior types.String) types.String {
	w, okW := parseWireDate(wire)
	if !okW {
		return types.StringValue(wire)
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		if p, ok := parseWireDate(prior.ValueString()); ok && p.Equal(w) {
			return prior
		}
	}
	return types.StringValue(w.Format(scheduleDateLayout))
}

// scheduleBody builds the write body (stored REST field names) from the model.
func scheduleBody(ctx context.Context, m *scheduleModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := map[string]any{"Name": m.Name.ValueString()}
	w, d := m.weekly(ctx)
	diags.Append(d...)
	f, d := m.fixed(ctx)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}
	switch {
	case w != nil:
		ws := map[string]any{
			"IsComplemented":  w.Complement.ValueBool(),
			"RecurrenceRange": map[string]any{"Type": "NoEndDate", "Start": "0001-01-01T00:00:00+00:00"},
		}
		for _, day := range w.days() {
			hours := make([]any, 0, len(*day.ranges))
			for _, rg := range *day.ranges {
				hours = append(hours, map[string]any{"Start": clockWire(rg.Start.ValueString()), "End": clockWire(rg.End.ValueString())})
			}
			ws[day.wire] = hours
		}
		body["Type"] = scheduleTypeWeekly
		body["WeeklyRecurrentSchedule"] = ws
	case f != nil:
		ranges := make([]any, 0, len(f.DateRange))
		for _, rg := range f.DateRange {
			ranges = append(ranges, map[string]any{"Start": rg.Start.ValueString(), "End": rg.End.ValueString()})
		}
		body["Type"] = scheduleTypeFixed
		body["FixedSchedule"] = map[string]any{"DateTimeRanges": ranges}
	default:
		diags.AddError("Invalid schedule configuration", "Exactly one of the weekly or fixed blocks must be set.")
	}
	return body, diags
}

// scheduleRanges maps read-back {Start, End} ranges onto range models, keeping
// prior values that denote the same time. An empty list stays null unless the
// prior value was an empty list.
func scheduleRanges(v any, prior []scheduleRangeModel, value func(string, types.String) types.String) []scheduleRangeModel {
	items, _ := v.([]any)
	if len(items) == 0 {
		if prior != nil {
			return []scheduleRangeModel{}
		}
		return nil
	}
	out := make([]scheduleRangeModel, 0, len(items))
	for i, it := range items {
		rg, _ := it.(map[string]any)
		p := scheduleRangeModel{Start: types.StringNull(), End: types.StringNull()}
		if i < len(prior) {
			p = prior[i]
		}
		out = append(out, scheduleRangeModel{
			Start: value(getString(rg, "Start"), p.Start),
			End:   value(getString(rg, "End"), p.End),
		})
	}
	return out
}

// scheduleModelFromObject maps a stored schedule object onto the model.
func scheduleModelFromObject(ctx context.Context, obj map[string]any, m *scheduleModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if id := voiceAppID(obj); id != "" {
		m.ID = types.StringValue(id)
	}
	readString(obj, "Name", &m.Name)
	priorW, _ := m.weekly(ctx)
	priorF, _ := m.fixed(ctx)
	typ := getString(obj, "Type")
	switch {
	case strings.EqualFold(typ, scheduleTypeWeekly):
		ws, _ := obj["WeeklyRecurrentSchedule"].(map[string]any)
		if priorW == nil {
			priorW = &scheduleWeeklyModel{Complement: types.BoolNull()}
		}
		w := scheduleWeeklyModel{Complement: priorW.Complement}
		if b, ok := ws["IsComplemented"].(bool); ok {
			if b || !w.Complement.IsNull() {
				w.Complement = types.BoolValue(b)
			}
		}
		if w.Complement.IsUnknown() {
			w.Complement = types.BoolNull()
		}
		priorDays := priorW.days()
		for i, day := range w.days() {
			*day.ranges = scheduleRanges(ws[day.wire], *priorDays[i].ranges, clockValue)
		}
		v, d := types.ObjectValueFrom(ctx, scheduleWeeklyAttrTypes, w)
		diags.Append(d...)
		m.Weekly = v
		m.Fixed = types.ObjectNull(scheduleFixedAttrTypes)
	case strings.EqualFold(typ, scheduleTypeFixed):
		fs, _ := obj["FixedSchedule"].(map[string]any)
		if priorF == nil {
			priorF = &scheduleFixedModel{}
		}
		f := scheduleFixedModel{DateRange: scheduleRanges(fs["DateTimeRanges"], priorF.DateRange, dateValue)}
		if f.DateRange == nil {
			f.DateRange = []scheduleRangeModel{}
		}
		v, d := types.ObjectValueFrom(ctx, scheduleFixedAttrTypes, f)
		diags.Append(d...)
		m.Fixed = v
		m.Weekly = types.ObjectNull(scheduleWeeklyAttrTypes)
	default:
		diags.AddError("Unexpected Type value", fmt.Sprintf("Teams returned Type = %v, which this provider does not recognise.", obj["Type"]))
		return diags
	}
	var d diag.Diagnostics
	m.AssociatedConfigurationIDs, d = stringListValue(stringsOf(obj["AssociatedConfigurationIds"]))
	diags.Append(d...)
	return diags
}
