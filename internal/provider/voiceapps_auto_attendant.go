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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
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
	aaAudioFile          = "AudioFile"
	aaAssocAfterHours    = "AfterHours"
	aaAssocHoliday       = "Holiday"
	aaAfterHoursPriority = 1
	aaHolidayPriority    = 100
	// aaDefaultCallPriority is the call priority Teams stores when none is given.
	aaDefaultCallPriority = 3
	aaDialScopeDefault    = "Default"
	aaDialScopeGroup      = "Group"
)

type autoAttendantModel struct {
	ID                       types.String `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	LanguageID               types.String `tfsdk:"language_id"`
	TimeZoneID               types.String `tfsdk:"time_zone_id"`
	VoiceID                  types.String `tfsdk:"voice_id"`
	Operator                 types.Object `tfsdk:"operator"`
	EnableVoiceResponse      types.Bool   `tfsdk:"enable_voice_response"`
	AuthorizedUsers          types.List   `tfsdk:"authorized_users"`
	HideAuthorizedUsers      types.List   `tfsdk:"hide_authorized_users"`
	UserNameExtension        types.String `tfsdk:"user_name_extension"`
	DialScopeIncludeGroupIDs types.List   `tfsdk:"dial_scope_include_group_ids"`
	DialScopeExcludeGroupIDs types.List   `tfsdk:"dial_scope_exclude_group_ids"`
	DefaultCallFlow          types.Object `tfsdk:"default_call_flow"`
	AfterHours               types.Object `tfsdk:"after_hours"`
	Holiday                  types.List   `tfsdk:"holiday"`
	ApplicationInstances     types.List   `tfsdk:"application_instances"`
}

type aaOperatorModel struct {
	TargetID             types.String `tfsdk:"target_id"`
	TargetType           types.String `tfsdk:"target_type"`
	CallPriority         types.Int64  `tfsdk:"call_priority"`
	EnableTranscription  types.Bool   `tfsdk:"enable_transcription"`
	SuppressSystemPrompt types.Bool   `tfsdk:"enable_shared_voicemail_system_prompt_suppression"`
}

type aaMenuOptionModel struct {
	Key                     types.String `tfsdk:"key"`
	Action                  types.String `tfsdk:"action"`
	TargetID                types.String `tfsdk:"target_id"`
	TargetType              types.String `tfsdk:"target_type"`
	Announcement            types.String `tfsdk:"announcement"`
	AnnouncementAudioFileID types.String `tfsdk:"announcement_audio_file_id"`
	SuppressSystemPrompt    types.Bool   `tfsdk:"enable_shared_voicemail_system_prompt_suppression"`
	EnableTranscription     types.Bool   `tfsdk:"enable_transcription"`
	CallPriority            types.Int64  `tfsdk:"call_priority"`
	VoiceResponses          types.List   `tfsdk:"voice_responses"`
}

type aaCallFlowModel struct {
	ID                           types.String        `tfsdk:"id"`
	Greeting                     types.String        `tfsdk:"greeting"`
	GreetingAudioFileID          types.String        `tfsdk:"greeting_audio_file_id"`
	MenuPrompt                   types.String        `tfsdk:"menu_prompt"`
	MenuPromptAudioFileID        types.String        `tfsdk:"menu_prompt_audio_file_id"`
	DirectorySearchMethod        types.String        `tfsdk:"directory_search_method"`
	ForceListenMenu              types.Bool          `tfsdk:"force_listen_menu"`
	RingResourceAccountDelegates types.Bool          `tfsdk:"ring_resource_account_delegates"`
	MenuOption                   []aaMenuOptionModel `tfsdk:"menu_option"`
}

type aaScheduledCallFlowModel struct {
	ID                           types.String        `tfsdk:"id"`
	ScheduleID                   types.String        `tfsdk:"schedule_id"`
	Greeting                     types.String        `tfsdk:"greeting"`
	GreetingAudioFileID          types.String        `tfsdk:"greeting_audio_file_id"`
	MenuPrompt                   types.String        `tfsdk:"menu_prompt"`
	MenuPromptAudioFileID        types.String        `tfsdk:"menu_prompt_audio_file_id"`
	DirectorySearchMethod        types.String        `tfsdk:"directory_search_method"`
	ForceListenMenu              types.Bool          `tfsdk:"force_listen_menu"`
	RingResourceAccountDelegates types.Bool          `tfsdk:"ring_resource_account_delegates"`
	MenuOption                   []aaMenuOptionModel `tfsdk:"menu_option"`
}

func (s *aaScheduledCallFlowModel) flow() aaCallFlowModel {
	return aaCallFlowModel{
		ID: s.ID, Greeting: s.Greeting, GreetingAudioFileID: s.GreetingAudioFileID,
		MenuPrompt: s.MenuPrompt, MenuPromptAudioFileID: s.MenuPromptAudioFileID,
		DirectorySearchMethod: s.DirectorySearchMethod, ForceListenMenu: s.ForceListenMenu,
		RingResourceAccountDelegates: s.RingResourceAccountDelegates, MenuOption: s.MenuOption,
	}
}

func scheduledFlow(f aaCallFlowModel, scheduleID types.String) aaScheduledCallFlowModel {
	return aaScheduledCallFlowModel{
		ID: f.ID, ScheduleID: scheduleID, Greeting: f.Greeting, GreetingAudioFileID: f.GreetingAudioFileID,
		MenuPrompt: f.MenuPrompt, MenuPromptAudioFileID: f.MenuPromptAudioFileID,
		DirectorySearchMethod: f.DirectorySearchMethod, ForceListenMenu: f.ForceListenMenu,
		RingResourceAccountDelegates: f.RingResourceAccountDelegates, MenuOption: f.MenuOption,
	}
}

// aaFlows holds the decoded call flow blocks of the model.
type aaFlows struct {
	Default    aaCallFlowModel
	AfterHours *aaScheduledCallFlowModel
	Holidays   []aaScheduledCallFlowModel
}

var (
	aaMenuOptionType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"key":                        types.StringType,
		"action":                     types.StringType,
		"target_id":                  types.StringType,
		"target_type":                types.StringType,
		"announcement":               types.StringType,
		"announcement_audio_file_id": types.StringType,
		"enable_shared_voicemail_system_prompt_suppression": types.BoolType,
		"enable_transcription":                              types.BoolType,
		"call_priority":                                     types.Int64Type,
		"voice_responses":                                   types.ListType{ElemType: types.StringType},
	}}
	aaCallFlowAttrTypes = map[string]attr.Type{
		"id":                              types.StringType,
		"greeting":                        types.StringType,
		"greeting_audio_file_id":          types.StringType,
		"menu_prompt":                     types.StringType,
		"menu_prompt_audio_file_id":       types.StringType,
		"directory_search_method":         types.StringType,
		"force_listen_menu":               types.BoolType,
		"ring_resource_account_delegates": types.BoolType,
		"menu_option":                     types.ListType{ElemType: aaMenuOptionType},
	}
	aaScheduledCallFlowAttrTypes = map[string]attr.Type{
		"id":                              types.StringType,
		"schedule_id":                     types.StringType,
		"greeting":                        types.StringType,
		"greeting_audio_file_id":          types.StringType,
		"menu_prompt":                     types.StringType,
		"menu_prompt_audio_file_id":       types.StringType,
		"directory_search_method":         types.StringType,
		"force_listen_menu":               types.BoolType,
		"ring_resource_account_delegates": types.BoolType,
		"menu_option":                     types.ListType{ElemType: aaMenuOptionType},
	}
	aaOperatorAttrTypes = map[string]attr.Type{
		"target_id":            types.StringType,
		"target_type":          types.StringType,
		"call_priority":        types.Int64Type,
		"enable_transcription": types.BoolType,
		"enable_shared_voicemail_system_prompt_suppression": types.BoolType,
	}
)

// operator decodes the operator attribute. Null or unknown reads as nil.
func (m *autoAttendantModel) operator(ctx context.Context) (*aaOperatorModel, diag.Diagnostics) {
	if m.Operator.IsNull() || m.Operator.IsUnknown() {
		return nil, nil
	}
	var o aaOperatorModel
	diags := m.Operator.As(ctx, &o, basetypes.ObjectAsOptions{})
	return &o, diags
}

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
		"id":                              schema.StringAttribute{Computed: true, Description: "Call flow id, generated by the provider.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"greeting":                        schema.StringAttribute{Optional: true, Description: "Text-to-speech greeting played when the call flow starts. Conflicts with greeting_audio_file_id."},
		"greeting_audio_file_id":          schema.StringAttribute{Optional: true, Description: "Id of a teams_audio_file (usage auto_attendant) played as the greeting. Conflicts with greeting."},
		"menu_prompt":                     schema.StringAttribute{Optional: true, Description: "Text-to-speech prompt describing the menu options. Conflicts with menu_prompt_audio_file_id."},
		"menu_prompt_audio_file_id":       schema.StringAttribute{Optional: true, Description: "Id of a teams_audio_file (usage auto_attendant) played as the menu prompt. Conflicts with menu_prompt."},
		"directory_search_method":         schema.StringAttribute{Optional: true, Description: "Directory search callers can use from the menu: none, by_name or by_extension. Null means none. Not allowed with an automatic menu option.", Validators: []validator.String{aaDirectorySearchMethods.stringValidator()}},
		"force_listen_menu":               schema.BoolAttribute{Optional: true, Description: "Play the whole menu prompt before accepting a key press or voice response."},
		"ring_resource_account_delegates": schema.BoolAttribute{Optional: true, Description: "Ring the delegates of the auto attendant resource account."},
		"menu_option": schema.ListNestedAttribute{
			Optional:    true,
			Description: "Menu options. An automatic option must be the only one.",
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"key":                        schema.StringAttribute{Required: true, Description: "Key: automatic, 0-9, star or pound.", Validators: []validator.String{aaMenuKeys.stringValidator()}},
				"action":                     schema.StringAttribute{Required: true, Description: "Action: disconnect, transfer, transfer_to_operator or announcement.", Validators: []validator.String{aaMenuActions.stringValidator()}},
				"target_id":                  schema.StringAttribute{Optional: true, Description: "Transfer target: a tel: number, a user object id or a resource account object id. Required for transfer."},
				"target_type":                schema.StringAttribute{Optional: true, Description: "Transfer target type: user, application_endpoint, external_pstn, shared_voicemail, or voice_app (the id of a teams_call_queue or teams_auto_attendant). Required for transfer.", Validators: []validator.String{aaTargetTypes.stringValidator()}},
				"announcement":               schema.StringAttribute{Optional: true, Description: "Text-to-speech announcement. For announcement, set this or announcement_audio_file_id."},
				"announcement_audio_file_id": schema.StringAttribute{Optional: true, Description: "Id of a teams_audio_file (usage auto_attendant) played as the announcement. For announcement, set this or announcement."},
				"enable_shared_voicemail_system_prompt_suppression": schema.BoolAttribute{Optional: true, Description: "Skip the Teams \"please leave a message after the tone\" prompt. Only allowed when target_type is shared_voicemail."},
				"enable_transcription":                              schema.BoolAttribute{Optional: true, Description: "Transcribe shared voicemail left through this option. Only allowed when target_type is shared_voicemail."},
				"call_priority":                                     schema.Int64Attribute{Optional: true, Description: "Priority of the transferred call, 1 (highest) to 5. Teams uses 3 when unset. Only allowed when action is transfer.", Validators: []validator.Int64{int64Range{lo: 1, hi: 5}}},
				"voice_responses":                                   schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Words callers can say to choose this option. Teams stores them either way and uses them only when enable_voice_response is true."},
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
		Description: "Manages a Teams auto attendant: the greeting and key-press menu callers hear, plus the after-hours and holiday call flows. Schedules are referenced by id and managed with teams_schedule.",
		Attributes: map[string]schema.Attribute{
			"id":                    schema.StringAttribute{Computed: true, Description: "Auto attendant id.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":                  schema.StringAttribute{Required: true, Description: "Display name of the auto attendant."},
			"language_id":           schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("en-US"), Description: "Language used for text-to-speech prompts."},
			"time_zone_id":          schema.StringAttribute{Required: true, Description: "Windows time zone id, e.g. Central Standard Time."},
			"voice_id":              schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("female"), Description: "Text-to-speech voice: female or male.", Validators: []validator.String{aaVoices.stringValidator()}},
			"application_instances": schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Resource account ids linked to the auto attendant.", PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()}},
			"operator": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Operator that callers reach with a transfer_to_operator menu option.",
				Attributes: map[string]schema.Attribute{
					"target_id":            schema.StringAttribute{Required: true, Description: "Operator: a tel: number, a user object id or a resource account object id."},
					"target_type":          schema.StringAttribute{Required: true, Description: "Operator type: user, application_endpoint, external_pstn, shared_voicemail, or voice_app (the id of a teams_call_queue or teams_auto_attendant).", Validators: []validator.String{aaTargetTypes.stringValidator()}},
					"call_priority":        schema.Int64Attribute{Optional: true, Description: "Priority of calls transferred to the operator, 1 (highest) to 5. Teams uses 3 when unset.", Validators: []validator.Int64{int64Range{lo: 1, hi: 5}}},
					"enable_transcription": schema.BoolAttribute{Optional: true, Description: "Transcribe shared voicemail left for the operator. Only allowed when target_type is shared_voicemail."},
					"enable_shared_voicemail_system_prompt_suppression": schema.BoolAttribute{Optional: true, Description: "Skip the Teams \"please leave a message after the tone\" prompt. Only allowed when target_type is shared_voicemail."},
				},
			},
			"enable_voice_response":        schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Let callers say menu options instead of pressing keys."},
			"authorized_users":             schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "User object ids allowed to change the auto attendant greetings and schedules."},
			"hide_authorized_users":        schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "User object ids hidden from the authorized users list shown in Teams. Teams does not require them to be in authorized_users."},
			"user_name_extension":          schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("none"), Description: "Detail read out after each name in dial-by-name results: none, office or department.", Validators: []validator.String{aaUserNameExtensions.stringValidator()}},
			"dial_scope_include_group_ids": schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Group ids whose members callers can reach by name or extension. Null means the whole directory."},
			"dial_scope_exclude_group_ids": schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Group ids whose members callers cannot reach by name or extension."},
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

// autoAttendantUpdateBody overlays body onto the stored object for the PUT and
// fills audio file prompt names from the stored object.
func autoAttendantUpdateBody(cur, body map[string]any) map[string]any {
	merged := overlayObject(cur, body)
	// Schedules is read-only; the PUT omits it.
	delete(merged, "Schedules")
	// Operator replaces the stored operator instead of being overlaid.
	if op, ok := body["Operator"]; ok {
		merged["Operator"] = op
	}
	names := map[string]string{}
	aaAudioFileNames(cur, names)
	out, _ := aaFillAudioFileNames(merged, names).(map[string]any)
	return out
}

// aaAudioFileNames collects the FileName of each AudioFilePrompt in v by lower-case Id.
func aaAudioFileNames(v any, names map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		if afp, ok := t["AudioFilePrompt"].(map[string]any); ok {
			if id, name := getString(afp, "Id"), getString(afp, "FileName"); id != "" && name != "" {
				names[strings.ToLower(id)] = name
			}
		}
		for _, e := range t {
			aaAudioFileNames(e, names)
		}
	case []any:
		for _, e := range t {
			aaAudioFileNames(e, names)
		}
	}
}

// aaFillAudioFileNames returns a deep copy of v with FileName set on each
// AudioFilePrompt that lacks one and whose Id is in names.
func aaFillAudioFileNames(v any, names map[string]string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = aaFillAudioFileNames(e, names)
		}
		if afp, ok := out["AudioFilePrompt"].(map[string]any); ok && getString(afp, "FileName") == "" {
			if name := names[strings.ToLower(getString(afp, "Id"))]; name != "" {
				afp["FileName"] = name
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = aaFillAudioFileNames(e, names)
		}
		return out
	}
	return v
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
	op, d := m.operator(ctx)
	if !d.HasError() && op != nil {
		diags.Append(validateAAOperator(path.Root("operator"), op)...)
	}
	needOperator := m.Operator.IsNull()
	if !m.DefaultCallFlow.IsNull() && !m.DefaultCallFlow.IsUnknown() {
		diags.Append(validateAAPrompts(path.Root("default_call_flow"), f.Default)...)
		diags.Append(validateAAMenu(path.Root("default_call_flow"), f.Default.MenuOption)...)
		diags.Append(validateAADirectorySearch(path.Root("default_call_flow"), f.Default)...)
		if needOperator {
			diags.Append(validateAAOperatorUse(path.Root("default_call_flow"), f.Default.MenuOption)...)
		}
	}
	if f.AfterHours != nil {
		if f.AfterHours.ScheduleID.IsNull() {
			diags.AddAttributeError(path.Root("after_hours").AtName("schedule_id"), "Invalid auto attendant configuration", "schedule_id is required in the after_hours block.")
		}
		diags.Append(validateAAPrompts(path.Root("after_hours"), f.AfterHours.flow())...)
		diags.Append(validateAAMenu(path.Root("after_hours"), f.AfterHours.MenuOption)...)
		diags.Append(validateAADirectorySearch(path.Root("after_hours"), f.AfterHours.flow())...)
		if needOperator {
			diags.Append(validateAAOperatorUse(path.Root("after_hours"), f.AfterHours.MenuOption)...)
		}
	}
	for i, h := range f.Holidays {
		if h.ScheduleID.IsNull() {
			diags.AddAttributeError(path.Root("holiday").AtListIndex(i).AtName("schedule_id"), "Invalid auto attendant configuration", "schedule_id is required in each holiday block.")
		}
		diags.Append(validateAAPrompts(path.Root("holiday").AtListIndex(i), h.flow())...)
		diags.Append(validateAAMenu(path.Root("holiday").AtListIndex(i), h.MenuOption)...)
		diags.Append(validateAADirectorySearch(path.Root("holiday").AtListIndex(i), h.flow())...)
		if needOperator {
			diags.Append(validateAAOperatorUse(path.Root("holiday").AtListIndex(i), h.MenuOption)...)
		}
	}
	return diags
}

// validateAAOperator checks the operator flags against its target type.
func validateAAOperator(p path.Path, o *aaOperatorModel) diag.Diagnostics {
	var diags diag.Diagnostics
	const summary = "Invalid auto attendant configuration"
	if o.TargetType.IsUnknown() || o.TargetType.ValueString() == "shared_voicemail" {
		return diags
	}
	if !o.SuppressSystemPrompt.IsNull() && !o.SuppressSystemPrompt.IsUnknown() {
		diags.AddAttributeError(p.AtName("enable_shared_voicemail_system_prompt_suppression"), summary, "enable_shared_voicemail_system_prompt_suppression must be unset unless target_type is \"shared_voicemail\".")
	}
	if !o.EnableTranscription.IsNull() && !o.EnableTranscription.IsUnknown() {
		diags.AddAttributeError(p.AtName("enable_transcription"), summary, "enable_transcription must be unset unless target_type is \"shared_voicemail\".")
	}
	return diags
}

// validateAAOperatorUse reports transfer_to_operator menu options; call it only when operator is null.
func validateAAOperatorUse(p path.Path, opts []aaMenuOptionModel) diag.Diagnostics {
	var diags diag.Diagnostics
	for i, o := range opts {
		if isSet(o.Action) && o.Action.ValueString() == "transfer_to_operator" {
			diags.AddAttributeError(p.AtName("menu_option").AtListIndex(i).AtName("action"), "Invalid auto attendant configuration",
				"action \"transfer_to_operator\" requires the operator attribute.")
		}
	}
	return diags
}

// validateAADirectorySearch rejects a directory search method other than none on a menu with an automatic option.
func validateAADirectorySearch(p path.Path, f aaCallFlowModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if !isSet(f.DirectorySearchMethod) || f.DirectorySearchMethod.ValueString() == "none" {
		return diags
	}
	for _, o := range f.MenuOption {
		if isSet(o.Key) && o.Key.ValueString() == "automatic" {
			diags.AddAttributeError(p.AtName("directory_search_method"), "Invalid auto attendant configuration",
				"directory_search_method must be none or unset when the menu has an option with key \"automatic\".")
			return diags
		}
	}
	return diags
}

// isSet reports whether s is known and not null.
func isSet(s types.String) bool { return !s.IsNull() && !s.IsUnknown() }

// validateAAPrompts checks that each prompt is either text or an audio file, not both.
func validateAAPrompts(p path.Path, f aaCallFlowModel) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, pair := range []struct {
		text, audio       string
		textVal, audioVal types.String
	}{
		{"greeting", "greeting_audio_file_id", f.Greeting, f.GreetingAudioFileID},
		{"menu_prompt", "menu_prompt_audio_file_id", f.MenuPrompt, f.MenuPromptAudioFileID},
	} {
		if isSet(pair.textVal) && isSet(pair.audioVal) {
			diags.AddAttributeError(p.AtName(pair.audio), "Invalid auto attendant configuration",
				fmt.Sprintf("%s and %s cannot both be set.", pair.text, pair.audio))
		}
	}
	if aaMenuTakesKeys(f.MenuOption) && f.MenuPrompt.IsNull() && f.MenuPromptAudioFileID.IsNull() {
		diags.AddAttributeError(p.AtName("menu_prompt"), "Invalid auto attendant configuration",
			"A menu with key-press options needs menu_prompt or menu_prompt_audio_file_id.")
	}
	return diags
}

// aaMenuTakesKeys reports whether any menu option has a known key other than automatic.
func aaMenuTakesKeys(opts []aaMenuOptionModel) bool {
	for _, o := range opts {
		if isSet(o.Key) && o.Key.ValueString() != "automatic" {
			return true
		}
	}
	return false
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
			if o.Announcement.IsNull() && o.AnnouncementAudioFileID.IsNull() {
				diags.AddAttributeError(op.AtName("announcement"), summary, "one of announcement or announcement_audio_file_id is required when action is \"announcement\".")
			}
			if isSet(o.Announcement) && isSet(o.AnnouncementAudioFileID) {
				diags.AddAttributeError(op.AtName("announcement_audio_file_id"), summary, "announcement and announcement_audio_file_id cannot both be set.")
			}
		}
		if action != "announcement" && isSet(o.Announcement) {
			diags.AddAttributeError(op.AtName("announcement"), summary, "announcement must be unset unless action is \"announcement\".")
		}
		if action != "announcement" && isSet(o.AnnouncementAudioFileID) {
			diags.AddAttributeError(op.AtName("announcement_audio_file_id"), summary, "announcement_audio_file_id must be unset unless action is \"announcement\".")
		}
		if !o.SuppressSystemPrompt.IsNull() && !o.SuppressSystemPrompt.IsUnknown() && !o.TargetType.IsUnknown() && o.TargetType.ValueString() != "shared_voicemail" {
			diags.AddAttributeError(op.AtName("enable_shared_voicemail_system_prompt_suppression"), summary, "enable_shared_voicemail_system_prompt_suppression must be unset unless target_type is \"shared_voicemail\".")
		}
		if !o.EnableTranscription.IsNull() && !o.EnableTranscription.IsUnknown() && !o.TargetType.IsUnknown() && o.TargetType.ValueString() != "shared_voicemail" {
			diags.AddAttributeError(op.AtName("enable_transcription"), summary, "enable_transcription must be unset unless target_type is \"shared_voicemail\".")
		}
		if !o.CallPriority.IsNull() && !o.CallPriority.IsUnknown() && action != "transfer" {
			diags.AddAttributeError(op.AtName("call_priority"), summary, "call_priority must be unset unless action is \"transfer\".")
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
	op, d := m.operator(ctx)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}
	body, d := autoAttendantBody(m, op, &f)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}
	diags.Append(m.setFlows(ctx, f)...)
	return body, diags
}

// autoAttendantBody builds the write body (stored REST field names). Call flows
// without an id get a new one, written back into f.
func autoAttendantBody(m *autoAttendantModel, op *aaOperatorModel, f *aaFlows) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	name := m.Name.ValueString()
	body := map[string]any{
		"Name":                 name,
		"LanguageId":           m.LanguageID.ValueString(),
		"TimeZoneId":           m.TimeZoneID.ValueString(),
		"VoiceResponseEnabled": m.EnableVoiceResponse.ValueBool(),
		"AuthorizedUsers":      aaStringsWire(m.AuthorizedUsers),
		"HideAuthorizedUsers":  aaStringsWire(m.HideAuthorizedUsers),
		"DirectoryLookupScope": map[string]any{
			"InclusionScope": aaDialScopeWire(listStrings(m.DialScopeIncludeGroupIDs)),
			"ExclusionScope": aaDialScopeWire(listStrings(m.DialScopeExcludeGroupIDs)),
		},
	}
	if w, ok := aaVoices.toWire(m.VoiceID.ValueString()); ok {
		body["VoiceId"] = w
	} else {
		diags.AddAttributeError(path.Root("voice_id"), "Invalid voice_id", fmt.Sprintf("%q is not a known voice.", m.VoiceID.ValueString()))
	}
	if w, ok := aaUserNameExtensions.toWire(m.UserNameExtension.ValueString()); ok {
		body["UserNameExtension"] = w
	} else {
		diags.AddAttributeError(path.Root("user_name_extension"), "Invalid user_name_extension", fmt.Sprintf("%q is not a known user name extension.", m.UserNameExtension.ValueString()))
	}
	body["Operator"] = nil
	if op != nil {
		ent, d := aaOperatorWire(op, path.Root("operator"))
		diags.Append(d...)
		body["Operator"] = ent
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

// aaPromptsWire returns a single prompt list for the audio file or the text, or an empty list when both are null.
func aaPromptsWire(text, audioFileID types.String) []any {
	if p := aaPromptOrNil(text, audioFileID); p != nil {
		return []any{p}
	}
	return []any{}
}

// aaPromptOrNil returns an audio file prompt when audioFileID is set, else a
// text-to-speech prompt when text is set, else nil.
func aaPromptOrNil(text, audioFileID types.String) map[string]any {
	if isSet(audioFileID) {
		return aaAudioPromptWire(audioFileID.ValueString())
	}
	if isSet(text) {
		return aaPromptWire(text.ValueString())
	}
	return nil
}

func aaPromptWire(text string) map[string]any {
	return map[string]any{"ActiveType": aaTextToSpeech, "TextToSpeechPrompt": text}
}

func aaAudioPromptWire(id string) map[string]any {
	return map[string]any{"ActiveType": aaAudioFile, "AudioFilePrompt": map[string]any{"Id": id}}
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
			if !o.SuppressSystemPrompt.IsNull() && !o.SuppressSystemPrompt.IsUnknown() {
				target["EnableSharedVoicemailSystemPromptSuppression"] = o.SuppressSystemPrompt.ValueBool()
			}
			if !o.EnableTranscription.IsNull() && !o.EnableTranscription.IsUnknown() {
				target["EnableTranscription"] = o.EnableTranscription.ValueBool()
			}
			if !o.CallPriority.IsNull() && !o.CallPriority.IsUnknown() {
				target["CallPriority"] = o.CallPriority.ValueInt64()
			}
			opt["CallTarget"] = target
		}
		if p := aaPromptOrNil(o.Announcement, o.AnnouncementAudioFileID); p != nil {
			opt["Prompt"] = p
		}
		if vr := listStrings(o.VoiceResponses); vr != nil {
			opt["VoiceResponses"] = vr
		}
		opts = append(opts, opt)
	}
	method := "none"
	if isSet(f.DirectorySearchMethod) {
		method = f.DirectorySearchMethod.ValueString()
	}
	methodWire, ok := aaDirectorySearchMethods.toWire(method)
	if !ok {
		diags.AddAttributeError(p.AtName("directory_search_method"), "Invalid directory_search_method", fmt.Sprintf("%q is not a known directory search method.", method))
	}
	return map[string]any{
		"Id":                           f.ID.ValueString(),
		"Name":                         name,
		"Greetings":                    aaPromptsWire(f.Greeting, f.GreetingAudioFileID),
		"ForceListenMenuEnabled":       f.ForceListenMenu.ValueBool(),
		"RingResourceAccountDelegates": f.RingResourceAccountDelegates.ValueBool(),
		"Menu": map[string]any{
			"Name":                  name + " menu",
			"Prompts":               aaPromptsWire(f.MenuPrompt, f.MenuPromptAudioFileID),
			"MenuOptions":           opts,
			"DialByNameEnabled":     method == "by_name",
			"DirectorySearchMethod": methodWire,
		},
	}, diags
}

// aaOperatorWire returns the operator as a callable entity with every field set.
func aaOperatorWire(o *aaOperatorModel, p path.Path) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	priority := int64(aaDefaultCallPriority)
	if !o.CallPriority.IsNull() && !o.CallPriority.IsUnknown() {
		priority = o.CallPriority.ValueInt64()
	}
	ent := map[string]any{
		"Id":                  o.TargetID.ValueString(),
		"CallPriority":        priority,
		"EnableTranscription": o.EnableTranscription.ValueBool(),
		"EnableSharedVoicemailSystemPromptSuppression": o.SuppressSystemPrompt.ValueBool(),
	}
	if w, ok := aaTargetTypes.toWire(o.TargetType.ValueString()); ok {
		ent["Type"] = w
	} else {
		diags.AddAttributeError(p.AtName("target_type"), "Invalid target_type", fmt.Sprintf("%q is not a known target type.", o.TargetType.ValueString()))
	}
	return ent, diags
}

// aaStringsWire returns the list's strings, or an empty list when null or unknown.
func aaStringsWire(l types.List) []string {
	if ss := listStrings(l); ss != nil {
		return ss
	}
	return []string{}
}

// aaDialScopeWire returns a directory lookup scope: Default when ids is empty, else a group scope.
func aaDialScopeWire(ids []string) map[string]any {
	if len(ids) == 0 {
		return map[string]any{"Type": aaDialScopeDefault, "GroupDialScope": nil}
	}
	return map[string]any{"Type": aaDialScopeGroup, "GroupDialScope": map[string]any{"GroupIds": ids}}
}

// aaDialScopeFromWire maps a stored directory lookup scope to its group ids.
func aaDialScopeFromWire(v any, prior types.List) (types.List, diag.Diagnostics) {
	scope, _ := v.(map[string]any)
	if !strings.EqualFold(getString(scope, "Type"), aaDialScopeGroup) {
		return optListValue(nil, prior)
	}
	groups, _ := scope["GroupDialScope"].(map[string]any)
	return optListValue(stringsOf(groups["GroupIds"]), prior)
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
	voiceResponse, _ := obj["VoiceResponseEnabled"].(bool)
	m.EnableVoiceResponse = types.BoolValue(voiceResponse)
	userNameExt := types.StringValue("none")
	diags.Append(readEnum(obj, "UserNameExtension", "UserNameExtension", aaUserNameExtensions, true, &userNameExt)...)
	m.UserNameExtension = userNameExt
	var d diag.Diagnostics
	m.AuthorizedUsers, d = optListValue(stringsOf(obj["AuthorizedUsers"]), m.AuthorizedUsers)
	diags.Append(d...)
	m.HideAuthorizedUsers, d = optListValue(stringsOf(obj["HideAuthorizedUsers"]), m.HideAuthorizedUsers)
	diags.Append(d...)
	lookup, _ := obj["DirectoryLookupScope"].(map[string]any)
	m.DialScopeIncludeGroupIDs, d = aaDialScopeFromWire(lookup["InclusionScope"], m.DialScopeIncludeGroupIDs)
	diags.Append(d...)
	m.DialScopeExcludeGroupIDs, d = aaDialScopeFromWire(lookup["ExclusionScope"], m.DialScopeExcludeGroupIDs)
	diags.Append(d...)
	priorOp, _ := m.operator(ctx)
	op, d := aaOperatorFromWire(obj["Operator"], priorOp)
	diags.Append(d...)
	if op == nil {
		m.Operator = types.ObjectNull(aaOperatorAttrTypes)
	} else {
		m.Operator, d = types.ObjectValueFrom(ctx, aaOperatorAttrTypes, *op)
		diags.Append(d...)
	}

	prior, _ := m.flows(ctx)
	var f aaFlows
	def, _ := obj["DefaultCallFlow"].(map[string]any)
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
	out.Greeting, out.GreetingAudioFileID, d = aaPromptFromWire(flow["Greetings"], prior.Greeting, prior.GreetingAudioFileID, field+".Greetings")
	diags.Append(d...)
	menu, _ := flow["Menu"].(map[string]any)
	out.MenuPrompt, out.MenuPromptAudioFileID, d = aaPromptFromWire(menu["Prompts"], prior.MenuPrompt, prior.MenuPromptAudioFileID, field+".Menu.Prompts")
	diags.Append(d...)
	out.DirectorySearchMethod, d = aaOptEnumValue(menu, "DirectorySearchMethod", field+".Menu.DirectorySearchMethod", aaDirectorySearchMethods, "none", prior.DirectorySearchMethod)
	diags.Append(d...)
	out.ForceListenMenu = aaOptBoolValue(flow["ForceListenMenuEnabled"], prior.ForceListenMenu)
	out.RingResourceAccountDelegates = aaOptBoolValue(flow["RingResourceAccountDelegates"], prior.RingResourceAccountDelegates)

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
			TargetType: types.StringNull(), Announcement: types.StringNull(), AnnouncementAudioFileID: types.StringNull(),
			SuppressSystemPrompt: types.BoolNull(), EnableTranscription: types.BoolNull(), CallPriority: types.Int64Null(),
		}
		diags.Append(readEnum(it, "DtmfResponse", of+".DtmfResponse", aaMenuKeys, true, &o.Key)...)
		diags.Append(readEnum(it, "Action", of+".Action", aaMenuActions, true, &o.Action)...)
		p := priorByKey[o.Key.ValueString()]
		if ct, ok := it["CallTarget"].(map[string]any); ok {
			if tid := getString(ct, "Id"); tid != "" {
				o.TargetID = aaTargetValue(tid, p.TargetID)
				diags.Append(readEnum(ct, "Type", of+".CallTarget.Type", aaTargetTypes, true, &o.TargetType)...)
			}
			o.SuppressSystemPrompt = aaOptBoolValue(ct["EnableSharedVoicemailSystemPromptSuppression"], p.SuppressSystemPrompt)
			o.EnableTranscription = aaOptBoolValue(ct["EnableTranscription"], p.EnableTranscription)
			o.CallPriority = aaOptInt64Value(ct["CallPriority"], p.CallPriority, aaDefaultCallPriority)
		}
		if pm, ok := it["Prompt"].(map[string]any); ok {
			o.Announcement, o.AnnouncementAudioFileID, d = aaPromptObjectFromWire(pm, p.Announcement, p.AnnouncementAudioFileID, of+".Prompt")
			diags.Append(d...)
		}
		o.VoiceResponses, d = optListValue(stringsOf(it["VoiceResponses"]), p.VoiceResponses)
		diags.Append(d...)
		out.MenuOption = append(out.MenuOption, o)
	}
	return out, diags
}

// aaPromptFromWire maps a stored prompt list to its text-to-speech text or audio file id.
func aaPromptFromWire(v any, priorText, priorAudio types.String, field string) (types.String, types.String, diag.Diagnostics) {
	items := objectsOf(v)
	if len(items) == 0 {
		return types.StringNull(), types.StringNull(), nil
	}
	return aaPromptObjectFromWire(items[0], priorText, priorAudio, field)
}

// aaPromptObjectFromWire maps one stored prompt to its text-to-speech text or audio file id.
func aaPromptObjectFromWire(pm map[string]any, priorText, priorAudio types.String, field string) (types.String, types.String, diag.Diagnostics) {
	var diags diag.Diagnostics
	at := pm["ActiveType"]
	s, _ := at.(string)
	switch {
	case strings.EqualFold(s, aaTextToSpeech):
		return optStringValue(pm["TextToSpeechPrompt"], priorText), types.StringNull(), diags
	case strings.EqualFold(s, aaAudioFile):
		afp, _ := pm["AudioFilePrompt"].(map[string]any)
		id := optIDValue(afp["Id"], priorAudio)
		if id.IsNull() {
			diags.AddError("Unexpected "+field+".AudioFilePrompt value", fmt.Sprintf("Teams returned %s.ActiveType = AudioFile without an AudioFilePrompt.Id.", field))
			return priorText, priorAudio, diags
		}
		return types.StringNull(), id, diags
	}
	diags.AddError("Unexpected "+field+".ActiveType value", fmt.Sprintf("Teams returned %s.ActiveType = %v; only TextToSpeech and AudioFile are supported.", field, at))
	return priorText, priorAudio, diags
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

// aaOptBoolValue maps a stored optional flag to the attribute value. A false or
// absent flag reads as null unless the prior value was set.
func aaOptBoolValue(v any, prior types.Bool) types.Bool {
	b, _ := v.(bool)
	if !b && (prior.IsNull() || prior.IsUnknown()) {
		return types.BoolNull()
	}
	return types.BoolValue(b)
}

// aaOptInt64Value maps a stored optional number to the attribute value. An absent
// number reads as def, and def reads as null unless the prior value was set.
func aaOptInt64Value(v any, prior types.Int64, def int64) types.Int64 {
	n := types.Int64Value(def)
	readInt64(map[string]any{"v": v}, "v", &n)
	if n.ValueInt64() == def && (prior.IsNull() || prior.IsUnknown()) {
		return types.Int64Null()
	}
	return n
}

// aaOptEnumValue maps a stored optional enum to the attribute value. An absent
// value reads as def, and def reads as null unless the prior value was set.
func aaOptEnumValue(obj map[string]any, key, field string, e wireEnum, def string, prior types.String) (types.String, diag.Diagnostics) {
	out := types.StringValue(def)
	diags := readEnum(obj, key, field, e, true, &out)
	if diags.HasError() {
		return prior, diags
	}
	if out.ValueString() == def && (prior.IsNull() || prior.IsUnknown()) {
		return types.StringNull(), diags
	}
	return out, diags
}

// aaOperatorFromWire maps a stored operator callable entity onto the operator model.
// A null operator or one without an id reads as nil.
func aaOperatorFromWire(v any, prior *aaOperatorModel) (*aaOperatorModel, diag.Diagnostics) {
	ent, _ := v.(map[string]any)
	id := getString(ent, "Id")
	if id == "" {
		return nil, nil
	}
	var p aaOperatorModel
	if prior != nil {
		p = *prior
	}
	o := aaOperatorModel{
		TargetID:             aaTargetValue(id, p.TargetID),
		TargetType:           types.StringNull(),
		CallPriority:         aaOptInt64Value(ent["CallPriority"], p.CallPriority, aaDefaultCallPriority),
		EnableTranscription:  aaOptBoolValue(ent["EnableTranscription"], p.EnableTranscription),
		SuppressSystemPrompt: aaOptBoolValue(ent["EnableSharedVoicemailSystemPromptSuppression"], p.SuppressSystemPrompt),
	}
	diags := readEnum(ent, "Type", "Operator.Type", aaTargetTypes, true, &o.TargetType)
	return &o, diags
}

// Auto attendant enums declared with this resource.
var (
	aaDirectorySearchMethods = newWireEnum(
		[2]string{"none", "None"},
		[2]string{"by_name", "ByName"},
		[2]string{"by_extension", "ByExtension"},
	)
	aaUserNameExtensions = newWireEnum(
		[2]string{"none", "None"},
		[2]string{"office", "Office"},
		[2]string{"department", "Department"},
	)
)
