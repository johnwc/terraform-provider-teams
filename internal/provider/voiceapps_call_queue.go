package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/terraform-provider-teams/internal/clients"
)

var (
	_ resource.Resource                   = &callQueueResource{}
	_ resource.ResourceWithConfigure      = &callQueueResource{}
	_ resource.ResourceWithImportState    = &callQueueResource{}
	_ resource.ResourceWithValidateConfig = &callQueueResource{}
)

type callQueueResource struct{ client *clients.Client }

// NewCallQueueResource manages a Teams call queue (New-CsCallQueue / Get-CsCallQueue / Set-CsCallQueue / Remove-CsCallQueue).
func NewCallQueueResource() resource.Resource { return &callQueueResource{} }

type callQueueModel struct {
	ID                                         types.String `tfsdk:"id"`
	Name                                       types.String `tfsdk:"name"`
	LanguageID                                 types.String `tfsdk:"language_id"`
	RoutingMethod                              types.String `tfsdk:"routing_method"`
	PresenceBasedRouting                       types.Bool   `tfsdk:"presence_based_routing"`
	ConferenceMode                             types.Bool   `tfsdk:"conference_mode"`
	AllowOptOut                                types.Bool   `tfsdk:"allow_opt_out"`
	AgentAlertTime                             types.Int64  `tfsdk:"agent_alert_time"`
	WelcomeTextToSpeechPrompt                  types.String `tfsdk:"welcome_text_to_speech_prompt"`
	Users                                      types.List   `tfsdk:"users"`
	DistributionLists                          types.List   `tfsdk:"distribution_lists"`
	AuthorizedUsers                            types.List   `tfsdk:"authorized_users"`
	OverflowThreshold                          types.Int64  `tfsdk:"overflow_threshold"`
	OverflowAction                             types.String `tfsdk:"overflow_action"`
	OverflowActionTarget                       types.String `tfsdk:"overflow_action_target"`
	OverflowSharedVoicemailTextToSpeechPrompt  types.String `tfsdk:"overflow_shared_voicemail_text_to_speech_prompt"`
	EnableOverflowSharedVoicemailTranscription types.Bool   `tfsdk:"enable_overflow_shared_voicemail_transcription"`
	OverflowDisconnectTextToSpeechPrompt       types.String `tfsdk:"overflow_disconnect_text_to_speech_prompt"`
	TimeoutThreshold                           types.Int64  `tfsdk:"timeout_threshold"`
	TimeoutAction                              types.String `tfsdk:"timeout_action"`
	TimeoutActionTarget                        types.String `tfsdk:"timeout_action_target"`
	TimeoutSharedVoicemailTextToSpeechPrompt   types.String `tfsdk:"timeout_shared_voicemail_text_to_speech_prompt"`
	EnableTimeoutSharedVoicemailTranscription  types.Bool   `tfsdk:"enable_timeout_shared_voicemail_transcription"`
	TimeoutDisconnectTextToSpeechPrompt        types.String `tfsdk:"timeout_disconnect_text_to_speech_prompt"`
	NoAgentAction                              types.String `tfsdk:"no_agent_action"`
	NoAgentActionTarget                        types.String `tfsdk:"no_agent_action_target"`
	NoAgentSharedVoicemailTextToSpeechPrompt   types.String `tfsdk:"no_agent_shared_voicemail_text_to_speech_prompt"`
	EnableNoAgentSharedVoicemailTranscription  types.Bool   `tfsdk:"enable_no_agent_shared_voicemail_transcription"`
	NoAgentDisconnectTextToSpeechPrompt        types.String `tfsdk:"no_agent_disconnect_text_to_speech_prompt"`
	ApplicationInstances                       types.List   `tfsdk:"application_instances"`
	OboResourceAccountIDs                      types.List   `tfsdk:"obo_resource_account_ids"`
}

// callQueueBranch groups the fields of one exception route (overflow, timeout, no agent).
type callQueueBranch struct {
	wire          string // Teams field prefix
	attr          string // terraform attribute prefix
	enum          wireEnum
	defaultAction string
	action        *types.String
	target        *types.String
	svPrompt      *types.String
	transcription *types.Bool
	discPrompt    *types.String
}

func (m *callQueueModel) branches() []callQueueBranch {
	return []callQueueBranch{
		{
			wire: "Overflow", attr: "overflow", enum: overflowActions, defaultAction: "disconnect_with_busy",
			action: &m.OverflowAction, target: &m.OverflowActionTarget, svPrompt: &m.OverflowSharedVoicemailTextToSpeechPrompt,
			transcription: &m.EnableOverflowSharedVoicemailTranscription, discPrompt: &m.OverflowDisconnectTextToSpeechPrompt,
		},
		{
			wire: "Timeout", attr: "timeout", enum: timeoutActions, defaultAction: "disconnect",
			action: &m.TimeoutAction, target: &m.TimeoutActionTarget, svPrompt: &m.TimeoutSharedVoicemailTextToSpeechPrompt,
			transcription: &m.EnableTimeoutSharedVoicemailTranscription, discPrompt: &m.TimeoutDisconnectTextToSpeechPrompt,
		},
		{
			wire: "NoAgent", attr: "no_agent", enum: noAgentActions, defaultAction: "queue",
			action: &m.NoAgentAction, target: &m.NoAgentActionTarget, svPrompt: &m.NoAgentSharedVoicemailTextToSpeechPrompt,
			transcription: &m.EnableNoAgentSharedVoicemailTranscription, discPrompt: &m.NoAgentDisconnectTextToSpeechPrompt,
		},
	}
}

// actionOrDefault returns the configured action, or the default when it is null.
func (b callQueueBranch) actionOrDefault() string {
	if b.action.IsNull() {
		return b.defaultAction
	}
	return b.action.ValueString()
}

func (r *callQueueResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_call_queue"
}

func (r *callQueueResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"id":                            schema.StringAttribute{Computed: true, Description: "Call queue identity (GUID).", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":                          schema.StringAttribute{Required: true, Description: "Display name of the call queue."},
		"language_id":                   schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("en-US"), Description: "Language used for text-to-speech prompts."},
		"routing_method":                schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("attendant"), Description: "Agent routing method: attendant, serial, round_robin or longest_idle.", Validators: []validator.String{routingMethods.stringValidator()}},
		"presence_based_routing":        schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Route only to agents whose presence is available. Not allowed with longest_idle."},
		"conference_mode":               schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Connect calls to agents using conference mode."},
		"allow_opt_out":                 schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Allow agents to opt out of taking calls."},
		"agent_alert_time":              schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(30), Description: "Seconds a call rings an agent before moving on (15-180).", Validators: []validator.Int64{int64Range{lo: 15, hi: 180}}},
		"welcome_text_to_speech_prompt": schema.StringAttribute{Optional: true, Description: "Text-to-speech greeting played to callers."},
		"users":                         schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Agent user object ids."},
		"distribution_lists":            schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Microsoft 365 group ids whose members are agents."},
		"authorized_users":              schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "User object ids allowed to manage the queue."},
		"overflow_threshold":            schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(50), Description: "Number of waiting calls that triggers overflow (0-200).", Validators: []validator.Int64{int64Range{lo: 0, hi: 200}}},
		"overflow_action":               schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("disconnect_with_busy"), Description: "Overflow action: disconnect_with_busy, forward, voicemail or shared_voicemail.", Validators: []validator.String{overflowActions.stringValidator()}},
		"timeout_threshold":             schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(1200), Description: "Seconds a call waits before timeout (0-2700).", Validators: []validator.Int64{int64Range{lo: 0, hi: 2700}}},
		"timeout_action":                schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("disconnect"), Description: "Timeout action: disconnect, forward, voicemail or shared_voicemail.", Validators: []validator.String{timeoutActions.stringValidator()}},
		"no_agent_action":               schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("queue"), Description: "Action when no agents are available: queue, disconnect, forward, voicemail or shared_voicemail.", Validators: []validator.String{noAgentActions.stringValidator()}},
		"application_instances":         schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Resource account ids linked to the queue.", PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()}},
		"obo_resource_account_ids":      schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Resource account ids agents can call on behalf of.", PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()}},
	}
	for _, b := range []struct{ attr, label string }{{"overflow", "overflow"}, {"timeout", "timeout"}, {"no_agent", "no agent"}} {
		attrs[b.attr+"_action_target"] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Target of the %s action: a tel: number, a user object id or a Microsoft 365 group id. Required for forward, voicemail and shared_voicemail.", b.label)}
		attrs[b.attr+"_shared_voicemail_text_to_speech_prompt"] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Shared voicemail greeting for the %s action. Required when the action is shared_voicemail; must be unset otherwise.", b.label)}
		attrs["enable_"+b.attr+"_shared_voicemail_transcription"] = schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: fmt.Sprintf("Transcribe %s shared voicemail.", b.label)}
		attrs[b.attr+"_disconnect_text_to_speech_prompt"] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Text-to-speech prompt played before the %s disconnect.", b.label)}
	}
	resp.Schema = schema.Schema{
		Description: "Manages a Teams call queue via New-CsCallQueue / Get-CsCallQueue / Set-CsCallQueue / Remove-CsCallQueue.",
		Attributes:  attrs,
	}
}

func (r *callQueueResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*clients.Client)
}

func (r *callQueueResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg callQueueModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateCallQueue(&cfg)...)
}

func (r *callQueueResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan callQueueModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, d := callQueueBody(&plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	createBody := createCallQueueBody(body)
	created, err := createVoiceApp(ctx, r.client.API, callQueueKind, createBody)
	if err != nil {
		resp.Diagnostics.AddError("New-CsCallQueue failed", err.Error())
		return
	}
	id := voiceAppID(created)
	if id == "" {
		resp.Diagnostics.AddError("New-CsCallQueue failed", "the response did not include the call queue identity")
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, callQueueKind, id)
	if err != nil {
		resp.Diagnostics.AddError("Get-CsCallQueue failed", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(callQueueModelFromObject(obj, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *callQueueResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state callQueueModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, callQueueKind, state.ID.ValueString())
	if err != nil {
		if isCallQueueGone(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Get-CsCallQueue failed", err.Error())
		return
	}
	resp.Diagnostics.Append(callQueueModelFromObject(obj, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *callQueueResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state callQueueModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	body, d := callQueueBody(&plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	cur, err := getVoiceApp(ctx, r.client.API, callQueueKind, id)
	if err != nil {
		resp.Diagnostics.AddError("Get-CsCallQueue failed", err.Error())
		return
	}
	if err := putVoiceApp(ctx, r.client.API, callQueueKind, id, callQueueUpdateBody(cur, body, &plan, &state)); err != nil {
		resp.Diagnostics.AddError("Set-CsCallQueue failed", err.Error())
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, callQueueKind, id)
	if err != nil {
		resp.Diagnostics.AddError("Get-CsCallQueue failed", err.Error())
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(callQueueModelFromObject(obj, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *callQueueResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state callQueueModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteVoiceApp(ctx, r.client.API, callQueueKind, state.ID.ValueString()); err != nil && !isCallQueueGone(err) {
		resp.Diagnostics.AddError("Remove-CsCallQueue failed", err.Error())
	}
}

func (r *callQueueResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// callQueueUpdateBody overlays body onto the stored object and empties each list that the plan unsets.
func callQueueUpdateBody(cur, body map[string]any, plan, state *callQueueModel) map[string]any {
	merged := overlayObject(cur, body)
	for _, l := range []struct {
		key         string
		plan, state types.List
	}{
		{"Agents", plan.Users, state.Users},
		{"Users", plan.Users, state.Users},
		{"DistributionLists", plan.DistributionLists, state.DistributionLists},
		{"AuthorizedUsers", plan.AuthorizedUsers, state.AuthorizedUsers},
	} {
		if l.plan.IsNull() && !l.state.IsNull() {
			merged[l.key] = []any{}
		}
	}
	return merged
}

// validateCallQueue checks the cross-attribute rules Teams enforces on write.
func validateCallQueue(m *callQueueModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if !m.RoutingMethod.IsUnknown() && !m.PresenceBasedRouting.IsUnknown() &&
		m.RoutingMethod.ValueString() == "longest_idle" && m.PresenceBasedRouting.ValueBool() {
		diags.AddAttributeError(path.Root("presence_based_routing"), "Invalid call queue configuration",
			"presence_based_routing cannot be true when routing_method is longest_idle.")
	}
	for _, b := range m.branches() {
		if b.action.IsUnknown() {
			continue
		}
		action := b.actionOrDefault()
		switch action {
		case "forward", "voicemail", "shared_voicemail":
			if b.target.IsNull() {
				diags.AddAttributeError(path.Root(b.attr+"_action_target"), "Invalid call queue configuration",
					fmt.Sprintf("%s_action_target is required when %s_action is %q.", b.attr, b.attr, action))
			}
		case "disconnect_with_busy", "disconnect", "queue":
			if !b.target.IsNull() && !b.target.IsUnknown() {
				diags.AddAttributeError(path.Root(b.attr+"_action_target"), "Invalid call queue configuration",
					fmt.Sprintf("%s_action_target must be unset when %s_action is %q.", b.attr, b.attr, action))
			}
		}
		promptAttr := b.attr + "_shared_voicemail_text_to_speech_prompt"
		if action == "shared_voicemail" && b.svPrompt.IsNull() {
			diags.AddAttributeError(path.Root(promptAttr), "Invalid call queue configuration",
				fmt.Sprintf("%s is required when %s_action is \"shared_voicemail\".", promptAttr, b.attr))
		}
		if action != "shared_voicemail" && !b.svPrompt.IsNull() && !b.svPrompt.IsUnknown() {
			diags.AddAttributeError(path.Root(promptAttr), "Invalid call queue configuration",
				fmt.Sprintf("%s must be unset unless %s_action is \"shared_voicemail\".", promptAttr, b.attr))
		}
	}
	return diags
}

// createCallQueueBody converts an update body's Agents into the Users id list the create endpoint takes.
func createCallQueueBody(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		out[k] = v
	}
	agents, ok := out["Agents"].([]any)
	if !ok {
		return out
	}
	delete(out, "Agents")
	ids := make([]string, 0, len(agents))
	for _, a := range agents {
		if am, ok := a.(map[string]any); ok {
			if id, ok := am["ObjectId"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	out["Users"] = ids
	return out
}

// callQueueBody builds the write body (stored REST field names) from the model.
func callQueueBody(m *callQueueModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := map[string]any{
		"Name":                      m.Name.ValueString(),
		"LanguageId":                m.LanguageID.ValueString(),
		"UseDefaultMusicOnHold":     true,
		"PresenceAwareRouting":      m.PresenceBasedRouting.ValueBool(),
		"ConferenceMode":            m.ConferenceMode.ValueBool(),
		"AllowOptOut":               m.AllowOptOut.ValueBool(),
		"AgentAlertTime":            m.AgentAlertTime.ValueInt64(),
		"OverflowThreshold":         m.OverflowThreshold.ValueInt64(),
		"TimeoutThreshold":          m.TimeoutThreshold.ValueInt64(),
		"WelcomeTextToSpeechPrompt": optStringWire(m.WelcomeTextToSpeechPrompt),
	}
	if w, ok := routingMethods.toWire(m.RoutingMethod.ValueString()); ok {
		body["RoutingMethod"] = w
	} else {
		diags.AddAttributeError(path.Root("routing_method"), "Invalid routing_method", fmt.Sprintf("%q is not a known routing method.", m.RoutingMethod.ValueString()))
	}
	for _, b := range m.branches() {
		action := b.actionOrDefault()
		w, ok := b.enum.toWire(action)
		if !ok {
			diags.AddAttributeError(path.Root(b.attr+"_action"), "Invalid "+b.attr+"_action", fmt.Sprintf("%q is not a known action.", action))
			continue
		}
		body[b.wire+"Action"] = w
		body[b.wire+"ActionTarget"] = optStringWire(*b.target)
		body[b.wire+"DisconnectTextToSpeechPrompt"] = optStringWire(*b.discPrompt)
		body["Enable"+b.wire+"SharedVoicemailTranscription"] = b.transcription.ValueBool()
		if action == "shared_voicemail" {
			body[b.wire+"SharedVoicemailTextToSpeechPrompt"] = optStringWire(*b.svPrompt)
		} else {
			body[b.wire+"SharedVoicemailTextToSpeechPrompt"] = nil
		}
	}
	if ids := listStrings(m.Users); ids != nil {
		agents := make([]any, 0, len(ids))
		for _, id := range ids {
			agents = append(agents, map[string]any{"ObjectId": id, "OptIn": true})
		}
		body["Agents"] = agents
		body["Users"] = ids
	}
	if ids := listStrings(m.DistributionLists); ids != nil {
		body["DistributionLists"] = ids
	}
	if ids := listStrings(m.AuthorizedUsers); ids != nil {
		body["AuthorizedUsers"] = ids
	}
	return body, diags
}

// callQueueModelFromObject maps a stored call queue object onto the model. Fields
// absent from the object keep their current model value.
func callQueueModelFromObject(obj map[string]any, m *callQueueModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if id := voiceAppID(obj); id != "" {
		m.ID = types.StringValue(id)
	}
	readString(obj, "Name", &m.Name)
	readString(obj, "LanguageId", &m.LanguageID)
	diags.Append(readEnum(obj, "RoutingMethod", "RoutingMethod", routingMethods, false, &m.RoutingMethod)...)
	readBool(obj, "PresenceAwareRouting", &m.PresenceBasedRouting)
	readBool(obj, "ConferenceMode", &m.ConferenceMode)
	readBool(obj, "AllowOptOut", &m.AllowOptOut)
	readInt64(obj, "AgentAlertTime", &m.AgentAlertTime)
	readInt64(obj, "OverflowThreshold", &m.OverflowThreshold)
	readInt64(obj, "TimeoutThreshold", &m.TimeoutThreshold)
	m.WelcomeTextToSpeechPrompt = optStringValue(obj["WelcomeTextToSpeechPrompt"], m.WelcomeTextToSpeechPrompt)
	for _, b := range m.branches() {
		diags.Append(readEnum(obj, b.wire+"Action", b.wire+"Action", b.enum, false, b.action)...)
		*b.target = callQueueTargetValue(obj[b.wire+"ActionTarget"], *b.target)
		*b.svPrompt = optStringValue(obj[b.wire+"SharedVoicemailTextToSpeechPrompt"], *b.svPrompt)
		*b.discPrompt = optStringValue(obj[b.wire+"DisconnectTextToSpeechPrompt"], *b.discPrompt)
		readBool(obj, "Enable"+b.wire+"SharedVoicemailTranscription", b.transcription)
	}
	var d diag.Diagnostics
	m.Users, d = optListValue(keepPriorOrder(callQueueAgentIDs(obj), m.Users), m.Users)
	diags.Append(d...)
	m.DistributionLists, d = optListValue(stringsOf(obj["DistributionLists"]), m.DistributionLists)
	diags.Append(d...)
	m.AuthorizedUsers, d = optListValue(stringsOf(obj["AuthorizedUsers"]), m.AuthorizedUsers)
	diags.Append(d...)
	m.ApplicationInstances, d = stringListValue(stringsOf(obj["ApplicationInstances"]))
	diags.Append(d...)
	m.OboResourceAccountIDs, d = stringListValue(stringsOf(obj["OboResourceAccountIds"]))
	diags.Append(d...)
	return diags
}

// callQueueTargetValue maps a read-back action target ({"Id": ...}, a string or
// null) to the attribute value. A prior "tel:" value matching a Phone Id is kept.
func callQueueTargetValue(v any, prior types.String) types.String {
	switch t := v.(type) {
	case string:
		if t == "" {
			return types.StringNull()
		}
		return types.StringValue(t)
	case map[string]any:
		id := getString(t, "Id")
		if id == "" {
			return types.StringNull()
		}
		if !prior.IsNull() && !prior.IsUnknown() && strings.EqualFold(prior.ValueString(), "tel:"+id) {
			return prior
		}
		return types.StringValue(id)
	}
	return types.StringNull()
}

// keepPriorOrder returns the prior list's order when it holds the same ids as ids.
func keepPriorOrder(ids []string, prior types.List) []string {
	priorIDs := listStrings(prior)
	if len(priorIDs) != len(ids) {
		return ids
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for _, id := range priorIDs {
		if !set[id] {
			return ids
		}
		delete(set, id)
	}
	if len(set) != 0 {
		return ids
	}
	return priorIDs
}

// callQueueAgentIDs returns the agent ids, falling back to Users when Agents is empty.
func callQueueAgentIDs(obj map[string]any) []string {
	if ids := agentIDs(obj["Agents"]); len(ids) > 0 {
		return ids
	}
	return stringsOf(obj["Users"])
}

func agentIDs(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		if a, ok := it.(map[string]any); ok {
			if id := getString(a, "ObjectId"); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}
