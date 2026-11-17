package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/terraprovider/terraform-provider-teams/internal/clients"
)

var (
	_ resource.Resource                   = &autoAttendantResource{}
	_ resource.ResourceWithConfigure      = &autoAttendantResource{}
	_ resource.ResourceWithImportState    = &autoAttendantResource{}
	_ resource.ResourceWithValidateConfig = &autoAttendantResource{}
)

type autoAttendantResource struct{ client *clients.Client }

// NewAutoAttendantResource manages a Teams auto attendant (New-CsAutoAttendant / Get-CsAutoAttendant / Set-CsAutoAttendant / Remove-CsAutoAttendant).
func NewAutoAttendantResource() resource.Resource { return &autoAttendantResource{} }

const (
	aaTextToSpeech       = "TextToSpeech"
	aaAssocAfterHours    = "AfterHours"
	aaAssocHoliday       = "Holiday"
	aaAfterHoursPriority = 1
	aaHolidayPriority    = 100
)

type autoAttendantModel struct {
	ID                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	LanguageID           types.String `tfsdk:"language_id"`
	TimeZoneID           types.String `tfsdk:"time_zone_id"`
	VoiceID              types.String `tfsdk:"voice_id"`
	DefaultCallFlow      types.Object `tfsdk:"default_call_flow"`
	AfterHours           types.Object `tfsdk:"after_hours"`
	Holiday              types.List   `tfsdk:"holiday"`
	ApplicationInstances types.List   `tfsdk:"application_instances"`
}

type aaMenuOptionModel struct {
	Key          types.String `tfsdk:"key"`
	Action       types.String `tfsdk:"action"`
	TargetID     types.String `tfsdk:"target_id"`
	TargetType   types.String `tfsdk:"target_type"`
	Announcement types.String `tfsdk:"announcement"`
}

type aaCallFlowModel struct {
	ID         types.String        `tfsdk:"id"`
	Greeting   types.String        `tfsdk:"greeting"`
	MenuPrompt types.String        `tfsdk:"menu_prompt"`
	MenuOption []aaMenuOptionModel `tfsdk:"menu_option"`
}

type aaScheduledCallFlowModel struct {
	ID         types.String        `tfsdk:"id"`
	ScheduleID types.String        `tfsdk:"schedule_id"`
	Greeting   types.String        `tfsdk:"greeting"`
	MenuPrompt types.String        `tfsdk:"menu_prompt"`
	MenuOption []aaMenuOptionModel `tfsdk:"menu_option"`
}

func (s *aaScheduledCallFlowModel) flow() aaCallFlowModel {
	return aaCallFlowModel{ID: s.ID, Greeting: s.Greeting, MenuPrompt: s.MenuPrompt, MenuOption: s.MenuOption}
}

func scheduledFlow(f aaCallFlowModel, scheduleID types.String) aaScheduledCallFlowModel {
	return aaScheduledCallFlowModel{ID: f.ID, ScheduleID: scheduleID, Greeting: f.Greeting, MenuPrompt: f.MenuPrompt, MenuOption: f.MenuOption}
}

// aaFlows holds the decoded call flow blocks of the model.
type aaFlows struct {
	Default    aaCallFlowModel
	AfterHours *aaScheduledCallFlowModel
	Holidays   []aaScheduledCallFlowModel
}

var (
	aaMenuOptionType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"key":          types.StringType,
		"action":       types.StringType,
		"target_id":    types.StringType,
		"target_type":  types.StringType,
		"announcement": types.StringType,
	}}
	aaCallFlowAttrTypes = map[string]attr.Type{
		"id":          types.StringType,
		"greeting":    types.StringType,
		"menu_prompt": types.StringType,
		"menu_option": types.ListType{ElemType: aaMenuOptionType},
	}
	aaScheduledCallFlowAttrTypes = map[string]attr.Type{
		"id":          types.StringType,
		"schedule_id": types.StringType,
		"greeting":    types.StringType,
		"menu_prompt": types.StringType,
		"menu_option": types.ListType{ElemType: aaMenuOptionType},
	}
)

// flows decodes the call flow blocks. Null or unknown blocks are left empty.
func (m *autoAttendantModel) flows(ctx context.Context) (aaFlows, diag.Diagnostics) {
	var f aaFlows
	var diags diag.Diagnostics
	if !m.DefaultCallFlow.IsNull() && !m.DefaultCallFlow.IsUnknown() {
		diags.Append(m.DefaultCallFlow.As(ctx, &f.Default, basetypes.ObjectAsOptions{})...)
	}
	if !m.AfterHours.IsNull() && !m.AfterHours.IsUnknown() {
		var ah aaScheduledCallFlowModel
		diags.Append(m.AfterHours.As(ctx, &ah, basetypes.ObjectAsOptions{})...)
		f.AfterHours = &ah
	}
	if !m.Holiday.IsNull() && !m.Holiday.IsUnknown() {
		diags.Append(m.Holiday.ElementsAs(ctx, &f.Holidays, false)...)
	}
	return f, diags
}

// setFlows writes the call flow blocks back onto the model.
func (m *autoAttendantModel) setFlows(ctx context.Context, f aaFlows) diag.Diagnostics {
	var diags diag.Diagnostics
	var d diag.Diagnostics
	m.DefaultCallFlow, d = types.ObjectValueFrom(ctx, aaCallFlowAttrTypes, f.Default)
	diags.Append(d...)
	if f.AfterHours == nil {
		m.AfterHours = types.ObjectNull(aaScheduledCallFlowAttrTypes)
	} else {
		m.AfterHours, d = types.ObjectValueFrom(ctx, aaScheduledCallFlowAttrTypes, *f.AfterHours)
		diags.Append(d...)
	}
	holidays := f.Holidays
	if holidays == nil {
		holidays = []aaScheduledCallFlowModel{}
	}
	m.Holiday, d = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: aaScheduledCallFlowAttrTypes}, holidays)
	diags.Append(d...)
	return diags
}

func (r *autoAttendantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auto_attendant"
}

// aaCallFlowAttributes returns the attributes of a call flow block.
func aaCallFlowAttributes(scheduled bool) map[string]schema.Attribute {
	attrs := map[string]schema.Attribute{
		"id":          schema.StringAttribute{Computed: true, Description: "Call flow id, generated by the provider.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"greeting":    schema.StringAttribute{Optional: true, Description: "Text-to-speech greeting played when the call flow starts."},
		"menu_prompt": schema.StringAttribute{Optional: true, Description: "Text-to-speech prompt describing the menu options."},
		"menu_option": schema.ListNestedAttribute{
			Optional:    true,
			Description: "Menu options. An automatic option must be the only one.",
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"key":          schema.StringAttribute{Required: true, Description: "Key: automatic, 0-9, star or pound.", Validators: []validator.String{aaMenuKeys.stringValidator()}},
				"action":       schema.StringAttribute{Required: true, Description: "Action: disconnect, transfer, transfer_to_operator or announcement.", Validators: []validator.String{aaMenuActions.stringValidator()}},
				"target_id":    schema.StringAttribute{Optional: true, Description: "Transfer target: a tel: number, a user object id or a resource account object id. Required for transfer."},
				"target_type":  schema.StringAttribute{Optional: true, Description: "Transfer target type: user, application_endpoint, external_pstn or shared_voicemail. Required for transfer.", Validators: []validator.String{aaTargetTypes.stringValidator()}},
				"announcement": schema.StringAttribute{Optional: true, Description: "Text-to-speech announcement. Required for announcement."},
			}},
		},
	}
	if scheduled {
		attrs["schedule_id"] = schema.StringAttribute{Optional: true, Description: "Id of the teams_schedule that activates this call flow. Required."}
	}
	return attrs
}

func (r *autoAttendantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Teams auto attendant via New-CsAutoAttendant / Get-CsAutoAttendant / Set-CsAutoAttendant / Remove-CsAutoAttendant. Schedules are referenced by id and managed with teams_schedule.",
		Attributes: map[string]schema.Attribute{
			"id":                    schema.StringAttribute{Computed: true, Description: "Auto attendant id.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":                  schema.StringAttribute{Required: true, Description: "Display name of the auto attendant."},
			"language_id":           schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("en-US"), Description: "Language used for text-to-speech prompts."},
			"time_zone_id":          schema.StringAttribute{Required: true, Description: "Windows time zone id, e.g. Central Standard Time."},
			"voice_id":              schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("female"), Description: "Text-to-speech voice: female or male.", Validators: []validator.String{aaVoices.stringValidator()}},
			"application_instances": schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Resource account ids linked to the auto attendant.", PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()}},
		},
		Blocks: map[string]schema.Block{
			"default_call_flow": schema.SingleNestedBlock{
				Description: "Call flow used outside after hours and holidays. Required.",
				Attributes:  aaCallFlowAttributes(false),
			},
			"after_hours": schema.SingleNestedBlock{
				Description: "Call flow used while the schedule is active.",
				Attributes:  aaCallFlowAttributes(true),
			},
			"holiday": schema.ListNestedBlock{
				Description:  "Holiday call flows, in priority order.",
				NestedObject: schema.NestedBlockObject{Attributes: aaCallFlowAttributes(true)},
			},
		},
	}
}

func (r *autoAttendantResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*clients.Client)
}

func (r *autoAttendantResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg autoAttendantModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateAutoAttendant(ctx, &cfg)...)
}

func (r *autoAttendantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan autoAttendantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, d := autoAttendantBodyFromModel(ctx, &plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := createVoiceApp(ctx, r.client.API, autoAttendantKind, body)
	if err != nil {
		resp.Diagnostics.AddError(autoAttendantKind.cmdlet("New")+" failed", err.Error())
		return
	}
	id := voiceAppID(created)
	if id == "" {
		resp.Diagnostics.AddError(autoAttendantKind.cmdlet("New")+" failed", "the response did not include the auto attendant id")
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, autoAttendantKind, id)
	if err != nil {
		resp.Diagnostics.AddError(autoAttendantKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(autoAttendantModelFromObject(ctx, obj, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *autoAttendantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state autoAttendantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, autoAttendantKind, state.ID.ValueString())
	if err != nil {
		if isAutoAttendantGone(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(autoAttendantKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	resp.Diagnostics.Append(autoAttendantModelFromObject(ctx, obj, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *autoAttendantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state autoAttendantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	body, d := autoAttendantBodyFromModel(ctx, &plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	cur, err := getVoiceApp(ctx, r.client.API, autoAttendantKind, id)
	if err != nil {
		resp.Diagnostics.AddError(autoAttendantKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	if err := putVoiceApp(ctx, r.client.API, autoAttendantKind, id, autoAttendantUpdateBody(cur, body)); err != nil {
		resp.Diagnostics.AddError(autoAttendantKind.cmdlet("Set")+" failed", err.Error())
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, autoAttendantKind, id)
	if err != nil {
		resp.Diagnostics.AddError(autoAttendantKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(autoAttendantModelFromObject(ctx, obj, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *autoAttendantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state autoAttendantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteVoiceApp(ctx, r.client.API, autoAttendantKind, state.ID.ValueString()); err != nil && !isAutoAttendantGone(err) {
		resp.Diagnostics.AddError(autoAttendantKind.cmdlet("Remove")+" failed", err.Error())
	}
}

func (r *autoAttendantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// autoAttendantUpdateBody overlays body onto the stored object for the PUT.
func autoAttendantUpdateBody(cur, body map[string]any) map[string]any {
	merged := overlayObject(cur, body)
	// Schedules is read-only; the PUT omits it.
	delete(merged, "Schedules")
	return merged
}

// validateAutoAttendant checks the menu rules Teams enforces on write. Blocks that
// are not yet known are skipped.
func validateAutoAttendant(ctx context.Context, m *autoAttendantModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if m.DefaultCallFlow.IsNull() {
		diags.AddAttributeError(path.Root("default_call_flow"), "Invalid auto attendant configuration", "The default_call_flow block is required.")
	}
	f, d := m.flows(ctx)
	if d.HasError() {
		return diags
	}
	if !m.DefaultCallFlow.IsNull() && !m.DefaultCallFlow.IsUnknown() {
		diags.Append(validateAAMenu(path.Root("default_call_flow"), f.Default.MenuOption)...)
	}
	if f.AfterHours != nil {
		if f.AfterHours.ScheduleID.IsNull() {
			diags.AddAttributeError(path.Root("after_hours").AtName("schedule_id"), "Invalid auto attendant configuration", "schedule_id is required in the after_hours block.")
		}
		diags.Append(validateAAMenu(path.Root("after_hours"), f.AfterHours.MenuOption)...)
	}
	for i, h := range f.Holidays {
		if h.ScheduleID.IsNull() {
			diags.AddAttributeError(path.Root("holiday").AtListIndex(i).AtName("schedule_id"), "Invalid auto attendant configuration", "schedule_id is required in each holiday block.")
		}
		diags.Append(validateAAMenu(path.Root("holiday").AtListIndex(i), h.MenuOption)...)
	}
	return diags
}

func validateAAMenu(p path.Path, opts []aaMenuOptionModel) diag.Diagnostics {
	var diags diag.Diagnostics
	const summary = "Invalid auto attendant configuration"
	seen := map[string]bool{}
	for i, o := range opts {
		op := p.AtName("menu_option").AtListIndex(i)
		if !o.Key.IsNull() && !o.Key.IsUnknown() {
			key := o.Key.ValueString()
			if seen[key] {
				diags.AddAttributeError(op.AtName("key"), summary, fmt.Sprintf("key %q is used by more than one menu option.", key))
			}
			seen[key] = true
			if key == "automatic" && len(opts) > 1 {
				diags.AddAttributeError(op.AtName("key"), summary, "a menu option with key \"automatic\" must be the only option in its menu.")
			}
		}
		if o.Action.IsNull() || o.Action.IsUnknown() {
			continue
		}
		action := o.Action.ValueString()
		switch action {
		case "transfer":
			if o.TargetID.IsNull() {
				diags.AddAttributeError(op.AtName("target_id"), summary, "target_id is required when action is \"transfer\".")
			}
			if o.TargetType.IsNull() {
				diags.AddAttributeError(op.AtName("target_type"), summary, "target_type is required when action is \"transfer\".")
			}
		case "disconnect", "transfer_to_operator":
			if action == "disconnect" && !o.Key.IsNull() && !o.Key.IsUnknown() && o.Key.ValueString() != "automatic" {
				diags.AddAttributeError(op.AtName("action"), summary, "action \"disconnect\" is only allowed with key \"automatic\".")
			}
			if !o.TargetID.IsNull() && !o.TargetID.IsUnknown() {
				diags.AddAttributeError(op.AtName("target_id"), summary, fmt.Sprintf("target_id must be unset when action is %q.", action))
			}
			if !o.TargetType.IsNull() && !o.TargetType.IsUnknown() {
				diags.AddAttributeError(op.AtName("target_type"), summary, fmt.Sprintf("target_type must be unset when action is %q.", action))
			}
		case "announcement":
			if !o.Key.IsNull() && !o.Key.IsUnknown() && o.Key.ValueString() == "automatic" {
				diags.AddAttributeError(op.AtName("key"), summary, "action \"announcement\" is not allowed with key \"automatic\".")
			}
			if o.Announcement.IsNull() {
				diags.AddAttributeError(op.AtName("announcement"), summary, "announcement is required when action is \"announcement\".")
			}
		}
		if action != "announcement" && !o.Announcement.IsNull() && !o.Announcement.IsUnknown() {
			diags.AddAttributeError(op.AtName("announcement"), summary, "announcement must be unset unless action is \"announcement\".")
		}
	}
	return diags
}

// ensureFlowID sets a new call flow id when id is null, unknown or empty.
func ensureFlowID(id *types.String) error {
	if id.IsNull() || id.IsUnknown() || id.ValueString() == "" {
		newID, err := newVoiceAppID()
		if err != nil {
			return err
		}
		*id = types.StringValue(newID)
	}
	return nil
}

// autoAttendantBodyFromModel decodes the call flow blocks, assigns ids to new
// call flows, writes them back onto the model and builds the write body.
func autoAttendantBodyFromModel(ctx context.Context, m *autoAttendantModel) (map[string]any, diag.Diagnostics) {
	f, diags := m.flows(ctx)
	if diags.HasError() {
		return nil, diags
	}
	body, d := autoAttendantBody(m, &f)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}
	diags.Append(m.setFlows(ctx, f)...)
	return body, diags
}

// autoAttendantBody builds the write body (stored REST field names). Call flows
// without an id get a new one, written back into f.
func autoAttendantBody(m *autoAttendantModel, f *aaFlows) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	name := m.Name.ValueString()
	body := map[string]any{
		"Name":       name,
		"LanguageId": m.LanguageID.ValueString(),
		"TimeZoneId": m.TimeZoneID.ValueString(),
	}
	if w, ok := aaVoices.toWire(m.VoiceID.ValueString()); ok {
		body["VoiceId"] = w
	} else {
		diags.AddAttributeError(path.Root("voice_id"), "Invalid voice_id", fmt.Sprintf("%q is not a known voice.", m.VoiceID.ValueString()))
	}

	if err := ensureFlowID(&f.Default.ID); err != nil {
		diags.AddError("Call flow id generation failed", err.Error())
		return nil, diags
	}
	def, d := aaCallFlowWire(name+" default", f.Default, path.Root("default_call_flow"))
	diags.Append(d...)
	body["DefaultCallFlow"] = def

	flows := []any{}
	assocs := []any{}
	if f.AfterHours != nil {
		if err := ensureFlowID(&f.AfterHours.ID); err != nil {
			diags.AddError("Call flow id generation failed", err.Error())
			return nil, diags
		}
		flow, d := aaCallFlowWire(name+" after hours", f.AfterHours.flow(), path.Root("after_hours"))
		diags.Append(d...)
		flows = append(flows, flow)
		assocs = append(assocs, aaAssociationWire(aaAssocAfterHours, f.AfterHours, aaAfterHoursPriority))
	}
	for i := range f.Holidays {
		h := &f.Holidays[i]
		if err := ensureFlowID(&h.ID); err != nil {
			diags.AddError("Call flow id generation failed", err.Error())
			return nil, diags
		}
		flow, d := aaCallFlowWire(fmt.Sprintf("%s holiday %d", name, i+1), h.flow(), path.Root("holiday").AtListIndex(i))
		diags.Append(d...)
		flows = append(flows, flow)
		assocs = append(assocs, aaAssociationWire(aaAssocHoliday, h, aaHolidayPriority))
	}
	body["CallFlows"] = flows
	body["CallHandlingAssociations"] = assocs
	return body, diags
}

func aaAssociationWire(typ string, f *aaScheduledCallFlowModel, priority int) map[string]any {
	return map[string]any{
		"Type":       typ,
		"ScheduleId": f.ScheduleID.ValueString(),
		"CallFlowId": f.ID.ValueString(),
		"Enabled":    true,
		"Priority":   priority,
	}
}

// aaPromptsWire returns a single text-to-speech prompt list, or an empty list when s is null.
func aaPromptsWire(s types.String) []any {
	if s.IsNull() || s.IsUnknown() {
		return []any{}
	}
	return []any{aaPromptWire(s.ValueString())}
}

func aaPromptWire(text string) map[string]any {
	return map[string]any{"ActiveType": aaTextToSpeech, "TextToSpeechPrompt": text}
}

func aaCallFlowWire(name string, f aaCallFlowModel, p path.Path) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	opts := make([]any, 0, len(f.MenuOption))
	for i, o := range f.MenuOption {
		op := p.AtName("menu_option").AtListIndex(i)
		opt := map[string]any{}
		if w, ok := aaMenuActions.toWire(o.Action.ValueString()); ok {
			opt["Action"] = w
		} else {
			diags.AddAttributeError(op.AtName("action"), "Invalid action", fmt.Sprintf("%q is not a known action.", o.Action.ValueString()))
		}
		if w, ok := aaMenuKeys.toWire(o.Key.ValueString()); ok {
			opt["DtmfResponse"] = w
		} else {
			diags.AddAttributeError(op.AtName("key"), "Invalid key", fmt.Sprintf("%q is not a known key.", o.Key.ValueString()))
		}
		if !o.TargetID.IsNull() && !o.TargetID.IsUnknown() {
			target := map[string]any{"Id": o.TargetID.ValueString()}
			if !o.TargetType.IsNull() && !o.TargetType.IsUnknown() {
				if w, ok := aaTargetTypes.toWire(o.TargetType.ValueString()); ok {
					target["Type"] = w
				} else {
					diags.AddAttributeError(op.AtName("target_type"), "Invalid target_type", fmt.Sprintf("%q is not a known target type.", o.TargetType.ValueString()))
				}
			}
			opt["CallTarget"] = target
		}
		if !o.Announcement.IsNull() && !o.Announcement.IsUnknown() {
			opt["Prompt"] = aaPromptWire(o.Announcement.ValueString())
		}
		opts = append(opts, opt)
	}
	return map[string]any{
		"Id":        f.ID.ValueString(),
		"Name":      name,
		"Greetings": aaPromptsWire(f.Greeting),
		"Menu": map[string]any{
			"Name":        name + " menu",
			"Prompts":     aaPromptsWire(f.MenuPrompt),
			"MenuOptions": opts,
		},
	}, diags
}

// autoAttendantModelFromObject maps a stored auto attendant object onto the model.
// Call flows are rebuilt from DefaultCallFlow, CallFlows and CallHandlingAssociations.
func autoAttendantModelFromObject(ctx context.Context, obj map[string]any, m *autoAttendantModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if id := voiceAppID(obj); id != "" {
		m.ID = types.StringValue(id)
	}
	readString(obj, "Name", &m.Name)
	readString(obj, "LanguageId", &m.LanguageID)
	readString(obj, "TimeZoneId", &m.TimeZoneID)
	diags.Append(readEnum(obj, "VoiceId", "VoiceId", aaVoices, true, &m.VoiceID)...)

	prior, _ := m.flows(ctx)
	var f aaFlows
	def, _ := obj["DefaultCallFlow"].(map[string]any)
	var d diag.Diagnostics
	f.Default, d = aaCallFlowFromWire(def, prior.Default, "DefaultCallFlow")
	diags.Append(d...)

	byID := map[string]map[string]any{}
	for _, it := range objectsOf(obj["CallFlows"]) {
		if id := getString(it, "Id"); id != "" {
			byID[strings.ToLower(id)] = it
		}
	}
	assocs := objectsOf(obj["CallHandlingAssociations"])
	sort.SliceStable(assocs, func(i, j int) bool { return getInt(assocs[i], "Priority") < getInt(assocs[j], "Priority") })
	for i, a := range assocs {
		field := fmt.Sprintf("CallHandlingAssociations[%d]", i)
		flowID := getString(a, "CallFlowId")
		flow, ok := byID[strings.ToLower(flowID)]
		if !ok {
			diags.AddError("Unexpected "+field+".CallFlowId value", fmt.Sprintf("Teams returned %s.CallFlowId = %q, which matches no call flow.", field, flowID))
			continue
		}
		scheduleID := types.StringValue(getString(a, "ScheduleId"))
		typ := getString(a, "Type")
		switch {
		case strings.EqualFold(typ, aaAssocAfterHours):
			if f.AfterHours != nil {
				diags.AddError("Unexpected "+field+".Type value", "Teams returned more than one AfterHours association.")
				continue
			}
			var p aaCallFlowModel
			if prior.AfterHours != nil {
				p = prior.AfterHours.flow()
			}
			cf, d := aaCallFlowFromWire(flow, p, "CallFlows["+flowID+"]")
			diags.Append(d...)
			ah := scheduledFlow(cf, scheduleID)
			f.AfterHours = &ah
		case strings.EqualFold(typ, aaAssocHoliday):
			var p aaCallFlowModel
			if n := len(f.Holidays); n < len(prior.Holidays) {
				p = prior.Holidays[n].flow()
			}
			cf, d := aaCallFlowFromWire(flow, p, "CallFlows["+flowID+"]")
			diags.Append(d...)
			f.Holidays = append(f.Holidays, scheduledFlow(cf, scheduleID))
		default:
			diags.AddError("Unexpected "+field+".Type value", fmt.Sprintf("Teams returned %s.Type = %v, which this provider does not recognise.", field, a["Type"]))
		}
	}
	if diags.HasError() {
		return diags
	}
	diags.Append(m.setFlows(ctx, f)...)
	m.ApplicationInstances, d = stringListValue(stringsOf(obj["ApplicationInstances"]))
	diags.Append(d...)
	return diags
}

// aaCallFlowFromWire maps a stored call flow onto a call flow model, keeping prior
// values that are equal in meaning.
func aaCallFlowFromWire(flow map[string]any, prior aaCallFlowModel, field string) (aaCallFlowModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := aaCallFlowModel{ID: types.StringNull()}
	if id := getString(flow, "Id"); id != "" {
		out.ID = types.StringValue(id)
	}
	var d diag.Diagnostics
	out.Greeting, d = aaPromptFromWire(flow["Greetings"], prior.Greeting, field+".Greetings")
	diags.Append(d...)
	menu, _ := flow["Menu"].(map[string]any)
	out.MenuPrompt, d = aaPromptFromWire(menu["Prompts"], prior.MenuPrompt, field+".Menu.Prompts")
	diags.Append(d...)

	priorByKey := map[string]aaMenuOptionModel{}
	for _, o := range prior.MenuOption {
		if !o.Key.IsNull() && !o.Key.IsUnknown() {
			priorByKey[o.Key.ValueString()] = o
		}
	}
	items := objectsOf(menu["MenuOptions"])
	if len(items) == 0 {
		if prior.MenuOption != nil {
			out.MenuOption = []aaMenuOptionModel{}
		}
		return out, diags
	}
	for i, it := range items {
		of := fmt.Sprintf("%s.Menu.MenuOptions[%d]", field, i)
		o := aaMenuOptionModel{
			Key: types.StringNull(), Action: types.StringNull(), TargetID: types.StringNull(),
			TargetType: types.StringNull(), Announcement: types.StringNull(),
		}
		diags.Append(readEnum(it, "DtmfResponse", of+".DtmfResponse", aaMenuKeys, true, &o.Key)...)
		diags.Append(readEnum(it, "Action", of+".Action", aaMenuActions, true, &o.Action)...)
		p := priorByKey[o.Key.ValueString()]
		if ct, ok := it["CallTarget"].(map[string]any); ok {
			if tid := getString(ct, "Id"); tid != "" {
				o.TargetID = aaTargetValue(tid, p.TargetID)
				diags.Append(readEnum(ct, "Type", of+".CallTarget.Type", aaTargetTypes, true, &o.TargetType)...)
			}
		}
		if pm, ok := it["Prompt"].(map[string]any); ok {
			o.Announcement = optStringValue(pm["TextToSpeechPrompt"], p.Announcement)
		}
		out.MenuOption = append(out.MenuOption, o)
	}
	return out, diags
}

// aaPromptFromWire maps a stored prompt list to its text-to-speech text. Only
// text-to-speech prompts are supported.
func aaPromptFromWire(v any, prior types.String, field string) (types.String, diag.Diagnostics) {
	var diags diag.Diagnostics
	items := objectsOf(v)
	if len(items) == 0 {
		return types.StringNull(), diags
	}
	at := items[0]["ActiveType"]
	if s, ok := at.(string); !ok || !strings.EqualFold(s, aaTextToSpeech) {
		diags.AddError("Unexpected "+field+".ActiveType value", fmt.Sprintf("Teams returned %s.ActiveType = %v; only TextToSpeech is supported.", field, at))
		return prior, diags
	}
	return optStringValue(items[0]["TextToSpeechPrompt"], prior), diags
}

// aaTargetValue maps a read-back call target id, keeping a prior value that
// differs only in case or a "tel:" prefix.
func aaTargetValue(id string, prior types.String) types.String {
	if !prior.IsNull() && !prior.IsUnknown() {
		trim := func(s string) string { return strings.TrimPrefix(strings.ToLower(s), "tel:") }
		if trim(prior.ValueString()) == trim(id) {
			return prior
		}
	}
	return types.StringValue(id)
}
