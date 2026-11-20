package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

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
	"github.com/terraprovider/go-teams/teamsapi"

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

// callQueueDefaultCallPriority is the call priority Teams applies when none is set.
const callQueueDefaultCallPriority = 3

type callQueueModel struct {
	ID                                                   types.String `tfsdk:"id"`
	Name                                                 types.String `tfsdk:"name"`
	LanguageID                                           types.String `tfsdk:"language_id"`
	RoutingMethod                                        types.String `tfsdk:"routing_method"`
	PresenceBasedRouting                                 types.Bool   `tfsdk:"presence_based_routing"`
	ConferenceMode                                       types.Bool   `tfsdk:"conference_mode"`
	AllowOptOut                                          types.Bool   `tfsdk:"allow_opt_out"`
	AgentAlertTime                                       types.Int64  `tfsdk:"agent_alert_time"`
	WelcomeTextToSpeechPrompt                            types.String `tfsdk:"welcome_text_to_speech_prompt"`
	WelcomeAudioFileID                                   types.String `tfsdk:"welcome_audio_file_id"`
	MusicOnHoldAudioFileID                               types.String `tfsdk:"music_on_hold_audio_file_id"`
	Users                                                types.List   `tfsdk:"users"`
	DistributionLists                                    types.List   `tfsdk:"distribution_lists"`
	AuthorizedUsers                                      types.List   `tfsdk:"authorized_users"`
	OverflowThreshold                                    types.Int64  `tfsdk:"overflow_threshold"`
	OverflowAction                                       types.String `tfsdk:"overflow_action"`
	OverflowActionTarget                                 types.String `tfsdk:"overflow_action_target"`
	OverflowSharedVoicemailTextToSpeechPrompt            types.String `tfsdk:"overflow_shared_voicemail_text_to_speech_prompt"`
	OverflowSharedVoicemailAudioFileID                   types.String `tfsdk:"overflow_shared_voicemail_audio_file_id"`
	EnableOverflowSharedVoicemailTranscription           types.Bool   `tfsdk:"enable_overflow_shared_voicemail_transcription"`
	EnableOverflowSharedVoicemailSystemPromptSuppression types.Bool   `tfsdk:"enable_overflow_shared_voicemail_system_prompt_suppression"`
	OverflowDisconnectTextToSpeechPrompt                 types.String `tfsdk:"overflow_disconnect_text_to_speech_prompt"`
	TimeoutThreshold                                     types.Int64  `tfsdk:"timeout_threshold"`
	TimeoutAction                                        types.String `tfsdk:"timeout_action"`
	TimeoutActionTarget                                  types.String `tfsdk:"timeout_action_target"`
	TimeoutSharedVoicemailTextToSpeechPrompt             types.String `tfsdk:"timeout_shared_voicemail_text_to_speech_prompt"`
	TimeoutSharedVoicemailAudioFileID                    types.String `tfsdk:"timeout_shared_voicemail_audio_file_id"`
	EnableTimeoutSharedVoicemailTranscription            types.Bool   `tfsdk:"enable_timeout_shared_voicemail_transcription"`
	EnableTimeoutSharedVoicemailSystemPromptSuppression  types.Bool   `tfsdk:"enable_timeout_shared_voicemail_system_prompt_suppression"`
	TimeoutDisconnectTextToSpeechPrompt                  types.String `tfsdk:"timeout_disconnect_text_to_speech_prompt"`
	NoAgentAction                                        types.String `tfsdk:"no_agent_action"`
	NoAgentActionTarget                                  types.String `tfsdk:"no_agent_action_target"`
	NoAgentSharedVoicemailTextToSpeechPrompt             types.String `tfsdk:"no_agent_shared_voicemail_text_to_speech_prompt"`
	NoAgentSharedVoicemailAudioFileID                    types.String `tfsdk:"no_agent_shared_voicemail_audio_file_id"`
	EnableNoAgentSharedVoicemailTranscription            types.Bool   `tfsdk:"enable_no_agent_shared_voicemail_transcription"`
	EnableNoAgentSharedVoicemailSystemPromptSuppression  types.Bool   `tfsdk:"enable_no_agent_shared_voicemail_system_prompt_suppression"`
	NoAgentDisconnectTextToSpeechPrompt                  types.String `tfsdk:"no_agent_disconnect_text_to_speech_prompt"`
	ChannelID                                            types.String `tfsdk:"channel_id"`
	ChannelUserObjectID                                  types.String `tfsdk:"channel_user_object_id"`
	OverwriteCallableChannel                             types.Bool   `tfsdk:"overwrite_callable_channel"`
	ApplicationInstances                                 types.List   `tfsdk:"application_instances"`
	OboResourceAccountIDs                                types.List   `tfsdk:"obo_resource_account_ids"`
	IsCallbackEnabled                                    types.Bool   `tfsdk:"is_callback_enabled"`
	CallbackRequestDtmf                                  types.String `tfsdk:"callback_request_dtmf"`
	WaitTimeBeforeOfferingCallbackInSecond               types.Int64  `tfsdk:"wait_time_before_offering_callback_in_second"`
	NumberOfCallsInQueueBeforeOfferingCallback           types.Int64  `tfsdk:"number_of_calls_in_queue_before_offering_callback"`
	CallToAgentRatioThresholdBeforeOfferingCallback      types.Int64  `tfsdk:"call_to_agent_ratio_threshold_before_offering_callback"`
	CallbackOfferTextToSpeechPrompt                      types.String `tfsdk:"callback_offer_text_to_speech_prompt"`
	CallbackOfferAudioFileID                             types.String `tfsdk:"callback_offer_audio_file_id"`
	CallbackEmailNotificationTarget                      types.String `tfsdk:"callback_email_notification_target"`
	OverflowRedirectPersonTextToSpeechPrompt             types.String `tfsdk:"overflow_redirect_person_text_to_speech_prompt"`
	OverflowRedirectPersonAudioFileID                    types.String `tfsdk:"overflow_redirect_person_audio_file_id"`
	OverflowRedirectPhoneNumberTextToSpeechPrompt        types.String `tfsdk:"overflow_redirect_phone_number_text_to_speech_prompt"`
	OverflowRedirectPhoneNumberAudioFileID               types.String `tfsdk:"overflow_redirect_phone_number_audio_file_id"`
	OverflowRedirectVoiceAppTextToSpeechPrompt           types.String `tfsdk:"overflow_redirect_voice_app_text_to_speech_prompt"`
	OverflowRedirectVoiceAppAudioFileID                  types.String `tfsdk:"overflow_redirect_voice_app_audio_file_id"`
	OverflowRedirectVoicemailTextToSpeechPrompt          types.String `tfsdk:"overflow_redirect_voicemail_text_to_speech_prompt"`
	OverflowRedirectVoicemailAudioFileID                 types.String `tfsdk:"overflow_redirect_voicemail_audio_file_id"`
	TimeoutRedirectPersonTextToSpeechPrompt              types.String `tfsdk:"timeout_redirect_person_text_to_speech_prompt"`
	TimeoutRedirectPersonAudioFileID                     types.String `tfsdk:"timeout_redirect_person_audio_file_id"`
	TimeoutRedirectPhoneNumberTextToSpeechPrompt         types.String `tfsdk:"timeout_redirect_phone_number_text_to_speech_prompt"`
	TimeoutRedirectPhoneNumberAudioFileID                types.String `tfsdk:"timeout_redirect_phone_number_audio_file_id"`
	TimeoutRedirectVoiceAppTextToSpeechPrompt            types.String `tfsdk:"timeout_redirect_voice_app_text_to_speech_prompt"`
	TimeoutRedirectVoiceAppAudioFileID                   types.String `tfsdk:"timeout_redirect_voice_app_audio_file_id"`
	TimeoutRedirectVoicemailTextToSpeechPrompt           types.String `tfsdk:"timeout_redirect_voicemail_text_to_speech_prompt"`
	TimeoutRedirectVoicemailAudioFileID                  types.String `tfsdk:"timeout_redirect_voicemail_audio_file_id"`
	NoAgentRedirectPersonTextToSpeechPrompt              types.String `tfsdk:"no_agent_redirect_person_text_to_speech_prompt"`
	NoAgentRedirectPersonAudioFileID                     types.String `tfsdk:"no_agent_redirect_person_audio_file_id"`
	NoAgentRedirectPhoneNumberTextToSpeechPrompt         types.String `tfsdk:"no_agent_redirect_phone_number_text_to_speech_prompt"`
	NoAgentRedirectPhoneNumberAudioFileID                types.String `tfsdk:"no_agent_redirect_phone_number_audio_file_id"`
	NoAgentRedirectVoiceAppTextToSpeechPrompt            types.String `tfsdk:"no_agent_redirect_voice_app_text_to_speech_prompt"`
	NoAgentRedirectVoiceAppAudioFileID                   types.String `tfsdk:"no_agent_redirect_voice_app_audio_file_id"`
	NoAgentRedirectVoicemailTextToSpeechPrompt           types.String `tfsdk:"no_agent_redirect_voicemail_text_to_speech_prompt"`
	NoAgentRedirectVoicemailAudioFileID                  types.String `tfsdk:"no_agent_redirect_voicemail_audio_file_id"`
	OverflowDisconnectAudioFileID                        types.String `tfsdk:"overflow_disconnect_audio_file_id"`
	TimeoutDisconnectAudioFileID                         types.String `tfsdk:"timeout_disconnect_audio_file_id"`
	NoAgentDisconnectAudioFileID                         types.String `tfsdk:"no_agent_disconnect_audio_file_id"`
	OverflowActionCallPriority                           types.Int64  `tfsdk:"overflow_action_call_priority"`
	TimeoutActionCallPriority                            types.Int64  `tfsdk:"timeout_action_call_priority"`
	NoAgentActionCallPriority                            types.Int64  `tfsdk:"no_agent_action_call_priority"`
	NoAgentApplyTo                                       types.String `tfsdk:"no_agent_apply_to"`
	HideAuthorizedUsers                                  types.List   `tfsdk:"hide_authorized_users"`
	ServiceLevelThresholdResponseTimeInSecond            types.Int64  `tfsdk:"service_level_threshold_response_time_in_second"`
	ShiftsTeamID                                         types.String `tfsdk:"shifts_team_id"`
	ShiftsSchedulingGroupID                              types.String `tfsdk:"shifts_scheduling_group_id"`
}

// callQueueRedirectKinds lists the redirect targets that have their own prompt, in callQueueBranch.redirect order.
var callQueueRedirectKinds = [4]struct{ attr, wire, action, target string }{
	{"person", "Person", "forward", "a person (the action target is a user object id)"},
	{"phone_number", "PhoneNumber", "forward", "a phone number (the action target is a tel: number)"},
	{"voice_app", "VoiceApp", "forward", "a voice app (the action target is a call queue or auto attendant id)"},
	{"voicemail", "Voicemail", "voicemail", "personal voicemail"},
}

// callQueueRedirectPrompt holds the text and audio prompt for one redirect kind.
type callQueueRedirectPrompt struct {
	tts, audio *types.String
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
	svAudio       *types.String
	transcription *types.Bool
	suppression   *types.Bool
	discPrompt    *types.String
	discAudio     *types.String
	priority      *types.Int64
	redirect      [4]callQueueRedirectPrompt
}

func (m *callQueueModel) branches() []callQueueBranch {
	return []callQueueBranch{
		{
			wire: "Overflow", attr: "overflow", enum: overflowActions, defaultAction: "disconnect_with_busy",
			action: &m.OverflowAction, target: &m.OverflowActionTarget, svPrompt: &m.OverflowSharedVoicemailTextToSpeechPrompt,
			svAudio:       &m.OverflowSharedVoicemailAudioFileID,
			transcription: &m.EnableOverflowSharedVoicemailTranscription, suppression: &m.EnableOverflowSharedVoicemailSystemPromptSuppression, discPrompt: &m.OverflowDisconnectTextToSpeechPrompt,
			discAudio: &m.OverflowDisconnectAudioFileID, priority: &m.OverflowActionCallPriority,
			redirect: [4]callQueueRedirectPrompt{
				{&m.OverflowRedirectPersonTextToSpeechPrompt, &m.OverflowRedirectPersonAudioFileID},
				{&m.OverflowRedirectPhoneNumberTextToSpeechPrompt, &m.OverflowRedirectPhoneNumberAudioFileID},
				{&m.OverflowRedirectVoiceAppTextToSpeechPrompt, &m.OverflowRedirectVoiceAppAudioFileID},
				{&m.OverflowRedirectVoicemailTextToSpeechPrompt, &m.OverflowRedirectVoicemailAudioFileID},
			},
		},
		{
			wire: "Timeout", attr: "timeout", enum: timeoutActions, defaultAction: "disconnect",
			action: &m.TimeoutAction, target: &m.TimeoutActionTarget, svPrompt: &m.TimeoutSharedVoicemailTextToSpeechPrompt,
			svAudio:       &m.TimeoutSharedVoicemailAudioFileID,
			transcription: &m.EnableTimeoutSharedVoicemailTranscription, suppression: &m.EnableTimeoutSharedVoicemailSystemPromptSuppression, discPrompt: &m.TimeoutDisconnectTextToSpeechPrompt,
			discAudio: &m.TimeoutDisconnectAudioFileID, priority: &m.TimeoutActionCallPriority,
			redirect: [4]callQueueRedirectPrompt{
				{&m.TimeoutRedirectPersonTextToSpeechPrompt, &m.TimeoutRedirectPersonAudioFileID},
				{&m.TimeoutRedirectPhoneNumberTextToSpeechPrompt, &m.TimeoutRedirectPhoneNumberAudioFileID},
				{&m.TimeoutRedirectVoiceAppTextToSpeechPrompt, &m.TimeoutRedirectVoiceAppAudioFileID},
				{&m.TimeoutRedirectVoicemailTextToSpeechPrompt, &m.TimeoutRedirectVoicemailAudioFileID},
			},
		},
		{
			wire: "NoAgent", attr: "no_agent", enum: noAgentActions, defaultAction: "queue",
			action: &m.NoAgentAction, target: &m.NoAgentActionTarget, svPrompt: &m.NoAgentSharedVoicemailTextToSpeechPrompt,
			svAudio:       &m.NoAgentSharedVoicemailAudioFileID,
			transcription: &m.EnableNoAgentSharedVoicemailTranscription, suppression: &m.EnableNoAgentSharedVoicemailSystemPromptSuppression, discPrompt: &m.NoAgentDisconnectTextToSpeechPrompt,
			discAudio: &m.NoAgentDisconnectAudioFileID, priority: &m.NoAgentActionCallPriority,
			redirect: [4]callQueueRedirectPrompt{
				{&m.NoAgentRedirectPersonTextToSpeechPrompt, &m.NoAgentRedirectPersonAudioFileID},
				{&m.NoAgentRedirectPhoneNumberTextToSpeechPrompt, &m.NoAgentRedirectPhoneNumberAudioFileID},
				{&m.NoAgentRedirectVoiceAppTextToSpeechPrompt, &m.NoAgentRedirectVoiceAppAudioFileID},
				{&m.NoAgentRedirectVoicemailTextToSpeechPrompt, &m.NoAgentRedirectVoicemailAudioFileID},
			},
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
		"welcome_text_to_speech_prompt": schema.StringAttribute{Optional: true, Description: "Text-to-speech greeting played to callers. Conflicts with welcome_audio_file_id."},
		"welcome_audio_file_id":         schema.StringAttribute{Optional: true, Description: "Id of a teams_audio_file (usage call_queue) played as the greeting. Conflicts with welcome_text_to_speech_prompt."},
		"music_on_hold_audio_file_id":   schema.StringAttribute{Optional: true, Description: "Id of a teams_audio_file (usage call_queue) played while callers wait. When unset, the default Teams music plays."},
		"users":                         schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Agent user object ids."},
		"distribution_lists":            schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Microsoft 365 group ids whose members are agents."},
		"channel_id":                    schema.StringAttribute{Optional: true, Description: "Thread id (19:...@thread.tacv2) of the Teams channel whose team members answer the queue. Requires channel_user_object_id, and distribution_lists set to the team's Microsoft 365 group id. Only standard channels are fully supported."},
		"channel_user_object_id":        schema.StringAttribute{Optional: true, Description: "Object id of a team owner that Teams uses to link the channel. Required with channel_id. Teams does not return it, so it is kept from configuration."},
		"overwrite_callable_channel":    schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Take the channel over when it is already linked to another call queue."},
		"authorized_users":              schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "User object ids allowed to manage the queue."},
		"overflow_threshold":            schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(50), Description: "Number of waiting calls that triggers overflow (0-200).", Validators: []validator.Int64{int64Range{lo: 0, hi: 200}}},
		"overflow_action":               schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("disconnect_with_busy"), Description: "Overflow action: disconnect_with_busy, forward, voicemail or shared_voicemail.", Validators: []validator.String{overflowActions.stringValidator()}},
		"timeout_threshold":             schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(1200), Description: "Seconds a call waits before timeout (0-2700).", Validators: []validator.Int64{int64Range{lo: 0, hi: 2700}}},
		"timeout_action":                schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("disconnect"), Description: "Timeout action: disconnect, forward, voicemail or shared_voicemail.", Validators: []validator.String{timeoutActions.stringValidator()}},
		"no_agent_action":               schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("queue"), Description: "Action when no agents are available: queue, disconnect, forward, voicemail or shared_voicemail.", Validators: []validator.String{noAgentActions.stringValidator()}},
		"application_instances":         schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Resource account ids linked to the queue.", PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()}},
		"obo_resource_account_ids":      schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Resource account ids agents can call on behalf of (outbound caller ID)."},
	}
	attrs["is_callback_enabled"] = schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Offer waiting callers a call back. Requires conference_mode true."}
	attrs["callback_request_dtmf"] = schema.StringAttribute{Optional: true, Description: "Key the caller presses to request a call back: tone0 to tone9, star or pound. Required when is_callback_enabled is true.", Validators: []validator.String{callbackDtmfKeys.stringValidator()}}
	attrs["wait_time_before_offering_callback_in_second"] = schema.Int64Attribute{Optional: true, Description: "Seconds a caller waits before a call back is offered."}
	attrs["number_of_calls_in_queue_before_offering_callback"] = schema.Int64Attribute{Optional: true, Description: "Number of calls in the queue at which a call back is offered."}
	attrs["call_to_agent_ratio_threshold_before_offering_callback"] = schema.Int64Attribute{Optional: true, Description: "Ratio of waiting calls to available agents at which a call back is offered."}
	attrs["callback_offer_text_to_speech_prompt"] = schema.StringAttribute{Optional: true, Description: "Text-to-speech prompt offering the call back. Conflicts with callback_offer_audio_file_id."}
	attrs["callback_offer_audio_file_id"] = schema.StringAttribute{Optional: true, Description: "Id of a teams_audio_file (usage call_queue) played to offer the call back. Conflicts with callback_offer_text_to_speech_prompt."}
	attrs["callback_email_notification_target"] = schema.StringAttribute{Optional: true, Description: "Microsoft 365 group id notified when a call back fails."}
	attrs["no_agent_apply_to"] = schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("all_calls"), Description: "Calls the no agent action applies to: all_calls or new_calls.", Validators: []validator.String{noAgentApplyTos.stringValidator()}}
	attrs["hide_authorized_users"] = schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Authorized user object ids not shown to agents as supervisors."}
	attrs["service_level_threshold_response_time_in_second"] = schema.Int64Attribute{Optional: true, Description: "Target seconds to answer a call, used for service level reporting."}
	attrs["shifts_team_id"] = schema.StringAttribute{Optional: true, Description: "Id of the team whose Shifts schedule supplies the agents. Requires shifts_scheduling_group_id. Conflicts with users, distribution_lists and channel_id."}
	attrs["shifts_scheduling_group_id"] = schema.StringAttribute{Optional: true, Description: "Id of the Shifts scheduling group whose members are agents. Requires shifts_team_id."}
	for _, b := range []struct{ attr, label string }{{"overflow", "overflow"}, {"timeout", "timeout"}, {"no_agent", "no agent"}} {
		for _, k := range callQueueRedirectKinds {
			ttsAttr := b.attr + "_redirect_" + k.attr + "_text_to_speech_prompt"
			audioAttr := b.attr + "_redirect_" + k.attr + "_audio_file_id"
			attrs[ttsAttr] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Text-to-speech prompt played before the %s action redirects the call to %s. Only allowed when %s_action is %s. Conflicts with %s.", b.label, k.target, b.attr, k.action, audioAttr)}
			attrs[audioAttr] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Id of a teams_audio_file (usage call_queue) played before the %s action redirects the call to %s. Only allowed when %s_action is %s. Conflicts with %s.", b.label, k.target, b.attr, k.action, ttsAttr)}
		}
		attrs[b.attr+"_disconnect_audio_file_id"] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Id of a teams_audio_file (usage call_queue) played before the %s disconnect. Conflicts with %s_disconnect_text_to_speech_prompt.", b.label, b.attr)}
		attrs[b.attr+"_action_call_priority"] = schema.Int64Attribute{Optional: true, Description: fmt.Sprintf("Priority (1 highest to 5 lowest) of the call when the %s action forwards it to a voice app. Only allowed when %s_action is forward.", b.label, b.attr), Validators: []validator.Int64{int64Range{lo: 1, hi: 5}}}
		attrs[b.attr+"_action_target"] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Target of the %s action: a tel: number, a user object id or a Microsoft 365 group id. Required for forward, voicemail and shared_voicemail.", b.label)}
		attrs[b.attr+"_shared_voicemail_text_to_speech_prompt"] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Text-to-speech shared voicemail greeting for the %s action. When the action is shared_voicemail, set this or %s_shared_voicemail_audio_file_id; must be unset otherwise.", b.label, b.attr)}
		attrs[b.attr+"_shared_voicemail_audio_file_id"] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Id of a teams_audio_file (usage call_queue) played as the shared voicemail greeting for the %s action. When the action is shared_voicemail, set this or %s_shared_voicemail_text_to_speech_prompt; must be unset otherwise.", b.label, b.attr)}
		attrs["enable_"+b.attr+"_shared_voicemail_transcription"] = schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: fmt.Sprintf("Transcribe %s shared voicemail.", b.label)}
		attrs["enable_"+b.attr+"_shared_voicemail_system_prompt_suppression"] = schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: fmt.Sprintf("Skip the Teams \"please leave a message after the tone\" prompt after the %s shared voicemail greeting. Only allowed when %s_action is shared_voicemail.", b.label, b.attr)}
		attrs[b.attr+"_disconnect_text_to_speech_prompt"] = schema.StringAttribute{Optional: true, Description: fmt.Sprintf("Text-to-speech prompt played before the %s disconnect.", b.label)}
	}
	resp.Schema = schema.Schema{
		Description: "Manages a Teams call queue: how calls ring the agents, and what happens on overflow, on timeout and when no agents are available.",
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
	created, err := createVoiceAppWithQuery(ctx, r.client.API, callQueueKind, callQueueChannelQuery(&plan), createBody)
	if err != nil {
		resp.Diagnostics.AddError("New-CsCallQueue failed", err.Error())
		return
	}
	id := voiceAppID(created)
	if id == "" {
		resp.Diagnostics.AddError("New-CsCallQueue failed", "the response did not include the call queue identity")
		return
	}
	obj, err := getCallQueueWithAgents(ctx, r.client.API, id, listStrings(plan.Users))
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
	if err := putVoiceAppWithQuery(ctx, r.client.API, callQueueKind, id, callQueueChannelQuery(&plan), callQueueUpdateBody(cur, body, &plan, &state)); err != nil {
		resp.Diagnostics.AddError("Set-CsCallQueue failed", err.Error())
		return
	}
	obj, err := getCallQueueWithAgents(ctx, r.client.API, id, listStrings(plan.Users))
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

// callQueueUpdateBody overlays body onto the stored object, sets the stored audio
// resource ids to the written audio file ids and empties each list that the plan unsets.
func callQueueUpdateBody(cur, body map[string]any, plan, state *callQueueModel) map[string]any {
	merged := overlayObject(cur, body)
	for resourceKey, fileKey := range map[string]string{
		"MusicOnHoldResourceId":  "MusicOnHoldAudioFileId",
		"WelcomeMusicResourceId": "WelcomeMusicAudioFileId",
	} {
		if _, ok := cur[resourceKey]; ok {
			merged[resourceKey] = body[fileKey]
		}
	}
	for _, l := range []struct {
		key         string
		plan, state types.List
	}{
		{"Agents", plan.Users, state.Users},
		{"Users", plan.Users, state.Users},
		{"DistributionLists", plan.DistributionLists, state.DistributionLists},
		{"AuthorizedUsers", plan.AuthorizedUsers, state.AuthorizedUsers},
		{"HideAuthorizedUsers", plan.HideAuthorizedUsers, state.HideAuthorizedUsers},
		{"OboResourceAccountIds", plan.OboResourceAccountIDs, state.OboResourceAccountIDs},
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
		audioAttr := b.attr + "_shared_voicemail_audio_file_id"
		if action == "shared_voicemail" {
			if b.svPrompt.IsNull() && b.svAudio.IsNull() {
				diags.AddAttributeError(path.Root(promptAttr), "Invalid call queue configuration",
					fmt.Sprintf("one of %s or %s is required when %s_action is \"shared_voicemail\".", promptAttr, audioAttr, b.attr))
			}
			if isSet(*b.svPrompt) && isSet(*b.svAudio) {
				diags.AddAttributeError(path.Root(audioAttr), "Invalid call queue configuration",
					fmt.Sprintf("%s and %s cannot both be set.", promptAttr, audioAttr))
			}
		} else {
			if !b.suppression.IsNull() && !b.suppression.IsUnknown() && b.suppression.ValueBool() {
				diags.AddAttributeError(path.Root("enable_"+b.attr+"_shared_voicemail_system_prompt_suppression"), "Invalid call queue configuration",
					fmt.Sprintf("enable_%s_shared_voicemail_system_prompt_suppression must be false unless %s_action is \"shared_voicemail\".", b.attr, b.attr))
			}
			for _, a := range []struct {
				name string
				val  types.String
			}{{promptAttr, *b.svPrompt}, {audioAttr, *b.svAudio}} {
				if isSet(a.val) {
					diags.AddAttributeError(path.Root(a.name), "Invalid call queue configuration",
						fmt.Sprintf("%s must be unset unless %s_action is \"shared_voicemail\".", a.name, b.attr))
				}
			}
		}
		if action != "forward" && !b.priority.IsNull() && !b.priority.IsUnknown() {
			diags.AddAttributeError(path.Root(b.attr+"_action_call_priority"), "Invalid call queue configuration",
				fmt.Sprintf("%s_action_call_priority must be unset unless %s_action is \"forward\".", b.attr, b.attr))
		}
		discAttr := b.attr + "_disconnect_audio_file_id"
		if isSet(*b.discPrompt) && isSet(*b.discAudio) {
			diags.AddAttributeError(path.Root(discAttr), "Invalid call queue configuration",
				fmt.Sprintf("%s_disconnect_text_to_speech_prompt and %s cannot both be set.", b.attr, discAttr))
		}
		for i, k := range callQueueRedirectKinds {
			r := b.redirect[i]
			prefix := b.attr + "_redirect_" + k.attr
			if isSet(*r.tts) && isSet(*r.audio) {
				diags.AddAttributeError(path.Root(prefix+"_audio_file_id"), "Invalid call queue configuration",
					fmt.Sprintf("%s_text_to_speech_prompt and %s_audio_file_id cannot both be set.", prefix, prefix))
			}
			if action != k.action {
				for _, a := range []struct {
					name string
					val  types.String
				}{{prefix + "_text_to_speech_prompt", *r.tts}, {prefix + "_audio_file_id", *r.audio}} {
					if isSet(a.val) {
						diags.AddAttributeError(path.Root(a.name), "Invalid call queue configuration",
							fmt.Sprintf("%s must be unset unless %s_action is %q.", a.name, b.attr, k.action))
					}
				}
			}
		}
	}
	diags.Append(validateCallQueueCallback(m)...)
	diags.Append(validateCallQueueShifts(m)...)
	if isSet(m.ChannelID) {
		if m.ChannelUserObjectID.IsNull() {
			diags.AddAttributeError(path.Root("channel_user_object_id"), "Invalid call queue configuration",
				"channel_user_object_id is required when channel_id is set.")
		}
		if !m.DistributionLists.IsUnknown() && len(m.DistributionLists.Elements()) != 1 {
			diags.AddAttributeError(path.Root("distribution_lists"), "Invalid call queue configuration",
				"distribution_lists must hold exactly the team's Microsoft 365 group id when channel_id is set.")
		}
	} else if m.ChannelID.IsNull() && isSet(m.ChannelUserObjectID) {
		diags.AddAttributeError(path.Root("channel_user_object_id"), "Invalid call queue configuration",
			"channel_user_object_id must be unset unless channel_id is set.")
	}
	if isSet(m.WelcomeTextToSpeechPrompt) && isSet(m.WelcomeAudioFileID) {
		diags.AddAttributeError(path.Root("welcome_audio_file_id"), "Invalid call queue configuration",
			"welcome_text_to_speech_prompt and welcome_audio_file_id cannot both be set.")
	}
	return diags
}

// validateCallQueueCallback checks the call back settings against is_callback_enabled.
func validateCallQueueCallback(m *callQueueModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if m.IsCallbackEnabled.IsUnknown() {
		return diags
	}
	thresholds := []struct {
		name string
		val  types.Int64
	}{
		{"wait_time_before_offering_callback_in_second", m.WaitTimeBeforeOfferingCallbackInSecond},
		{"number_of_calls_in_queue_before_offering_callback", m.NumberOfCallsInQueueBeforeOfferingCallback},
		{"call_to_agent_ratio_threshold_before_offering_callback", m.CallToAgentRatioThresholdBeforeOfferingCallback},
	}
	if m.IsCallbackEnabled.ValueBool() {
		if !m.ConferenceMode.IsUnknown() && !m.ConferenceMode.ValueBool() {
			diags.AddAttributeError(path.Root("conference_mode"), "Invalid call queue configuration",
				"conference_mode must be true when is_callback_enabled is true.")
		}
		if m.CallbackRequestDtmf.IsNull() {
			diags.AddAttributeError(path.Root("callback_request_dtmf"), "Invalid call queue configuration",
				"callback_request_dtmf is required when is_callback_enabled is true.")
		}
		if m.CallbackOfferTextToSpeechPrompt.IsNull() && m.CallbackOfferAudioFileID.IsNull() {
			diags.AddAttributeError(path.Root("callback_offer_text_to_speech_prompt"), "Invalid call queue configuration",
				"one of callback_offer_text_to_speech_prompt or callback_offer_audio_file_id is required when is_callback_enabled is true.")
		}
		if isSet(m.CallbackOfferTextToSpeechPrompt) && isSet(m.CallbackOfferAudioFileID) {
			diags.AddAttributeError(path.Root("callback_offer_audio_file_id"), "Invalid call queue configuration",
				"callback_offer_text_to_speech_prompt and callback_offer_audio_file_id cannot both be set.")
		}
		anyThreshold := false
		for _, t := range thresholds {
			if !t.val.IsNull() {
				anyThreshold = true
			}
		}
		if !anyThreshold {
			diags.AddAttributeError(path.Root("wait_time_before_offering_callback_in_second"), "Invalid call queue configuration",
				"one of wait_time_before_offering_callback_in_second, number_of_calls_in_queue_before_offering_callback or call_to_agent_ratio_threshold_before_offering_callback is required when is_callback_enabled is true.")
		}
		return diags
	}
	for _, s := range []struct {
		name string
		val  types.String
	}{
		{"callback_request_dtmf", m.CallbackRequestDtmf},
		{"callback_offer_text_to_speech_prompt", m.CallbackOfferTextToSpeechPrompt},
		{"callback_offer_audio_file_id", m.CallbackOfferAudioFileID},
	} {
		if isSet(s.val) {
			diags.AddAttributeError(path.Root(s.name), "Invalid call queue configuration",
				fmt.Sprintf("%s must be unset unless is_callback_enabled is true.", s.name))
		}
	}
	for _, t := range thresholds {
		if !t.val.IsNull() && !t.val.IsUnknown() {
			diags.AddAttributeError(path.Root(t.name), "Invalid call queue configuration",
				fmt.Sprintf("%s must be unset unless is_callback_enabled is true.", t.name))
		}
	}
	return diags
}

// validateCallQueueShifts checks that the Shifts team and scheduling group are set together and without other agent sources.
func validateCallQueueShifts(m *callQueueModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if isSet(m.ShiftsTeamID) && m.ShiftsSchedulingGroupID.IsNull() {
		diags.AddAttributeError(path.Root("shifts_scheduling_group_id"), "Invalid call queue configuration",
			"shifts_scheduling_group_id is required when shifts_team_id is set.")
	}
	if isSet(m.ShiftsSchedulingGroupID) && m.ShiftsTeamID.IsNull() {
		diags.AddAttributeError(path.Root("shifts_team_id"), "Invalid call queue configuration",
			"shifts_team_id is required when shifts_scheduling_group_id is set.")
	}
	if !isSet(m.ShiftsTeamID) && !isSet(m.ShiftsSchedulingGroupID) {
		return diags
	}
	for _, l := range []struct {
		name string
		val  types.List
	}{{"users", m.Users}, {"distribution_lists", m.DistributionLists}} {
		if !l.val.IsNull() && !l.val.IsUnknown() && len(l.val.Elements()) > 0 {
			diags.AddAttributeError(path.Root(l.name), "Invalid call queue configuration",
				fmt.Sprintf("%s cannot be set together with shifts_team_id and shifts_scheduling_group_id.", l.name))
		}
	}
	if isSet(m.ChannelID) {
		diags.AddAttributeError(path.Root("channel_id"), "Invalid call queue configuration",
			"channel_id cannot be set together with shifts_team_id and shifts_scheduling_group_id.")
	}
	return diags
}

// callQueueInt64Wire returns the integer value, or nil when null or unknown.
func callQueueInt64Wire(v types.Int64) any {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueInt64()
}

// callQueueChannelQuery returns the channelUserObjectId query Teams requires on writes that link a channel.
func callQueueChannelQuery(m *callQueueModel) string {
	if !isSet(m.ChannelID) || !isSet(m.ChannelUserObjectID) {
		return ""
	}
	return "?channelUserObjectId=" + url.QueryEscape(m.ChannelUserObjectID.ValueString())
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
		"Name":                                   m.Name.ValueString(),
		"LanguageId":                             m.LanguageID.ValueString(),
		"UseDefaultMusicOnHold":                  !isSet(m.MusicOnHoldAudioFileID),
		"MusicOnHoldAudioFileId":                 optStringWire(m.MusicOnHoldAudioFileID),
		"PresenceAwareRouting":                   m.PresenceBasedRouting.ValueBool(),
		"ConferenceMode":                         m.ConferenceMode.ValueBool(),
		"AllowOptOut":                            m.AllowOptOut.ValueBool(),
		"AgentAlertTime":                         m.AgentAlertTime.ValueInt64(),
		"OverflowThreshold":                      m.OverflowThreshold.ValueInt64(),
		"TimeoutThreshold":                       m.TimeoutThreshold.ValueInt64(),
		"WelcomeTextToSpeechPrompt":              optStringWire(m.WelcomeTextToSpeechPrompt),
		"WelcomeMusicAudioFileId":                optStringWire(m.WelcomeAudioFileID),
		"ThreadId":                               optStringWire(m.ChannelID),
		"ShouldOverwriteCallableChannelProperty": m.OverwriteCallableChannel.ValueBool(),
	}
	if w, ok := routingMethods.toWire(m.RoutingMethod.ValueString()); ok {
		body["RoutingMethod"] = w
	} else {
		diags.AddAttributeError(path.Root("routing_method"), "Invalid routing_method", fmt.Sprintf("%q is not a known routing method.", m.RoutingMethod.ValueString()))
	}
	body["IsCallbackEnabled"] = m.IsCallbackEnabled.ValueBool()
	body["CallbackRequestDtmf"] = nil
	if isSet(m.CallbackRequestDtmf) {
		if w, ok := callbackDtmfKeys.toWire(m.CallbackRequestDtmf.ValueString()); ok {
			body["CallbackRequestDtmf"] = w
		} else {
			diags.AddAttributeError(path.Root("callback_request_dtmf"), "Invalid callback_request_dtmf", fmt.Sprintf("%q is not a known key.", m.CallbackRequestDtmf.ValueString()))
		}
	}
	body["WaitTimeBeforeOfferingCallbackInSecond"] = callQueueInt64Wire(m.WaitTimeBeforeOfferingCallbackInSecond)
	body["NumberOfCallsInQueueBeforeOfferingCallback"] = callQueueInt64Wire(m.NumberOfCallsInQueueBeforeOfferingCallback)
	body["CallToAgentRatioThresholdBeforeOfferingCallback"] = callQueueInt64Wire(m.CallToAgentRatioThresholdBeforeOfferingCallback)
	body["CallbackOfferTextToSpeechPrompt"] = optStringWire(m.CallbackOfferTextToSpeechPrompt)
	body["CallbackOfferAudioFilePromptResourceId"] = optStringWire(m.CallbackOfferAudioFileID)
	body["CallbackEmailNotificationTarget"] = optStringWire(m.CallbackEmailNotificationTarget)
	body["ServiceLevelThresholdResponseTimeInSecond"] = callQueueInt64Wire(m.ServiceLevelThresholdResponseTimeInSecond)
	body["ShiftsTeamId"] = optStringWire(m.ShiftsTeamID)
	body["ShiftsSchedulingGroupId"] = optStringWire(m.ShiftsSchedulingGroupID)
	applyTo := "all_calls"
	if isSet(m.NoAgentApplyTo) {
		applyTo = m.NoAgentApplyTo.ValueString()
	}
	if n, ok := noAgentApplyToNumber(applyTo); ok {
		body["NoAgentApplyTo"] = n
	} else {
		diags.AddAttributeError(path.Root("no_agent_apply_to"), "Invalid no_agent_apply_to", fmt.Sprintf("%q is not a known value.", applyTo))
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
		body[b.wire+"DisconnectAudioFilePrompt"] = optStringWire(*b.discAudio)
		for i, k := range callQueueRedirectKinds {
			body[b.wire+"Redirect"+k.wire+"TextToSpeechPrompt"] = optStringWire(*b.redirect[i].tts)
			body[b.wire+"Redirect"+k.wire+"AudioFilePrompt"] = optStringWire(*b.redirect[i].audio)
		}
		if !b.priority.IsNull() && !b.priority.IsUnknown() {
			body[b.wire+"ActionCallPriority"] = b.priority.ValueInt64()
		} else {
			body[b.wire+"ActionCallPriority"] = int64(callQueueDefaultCallPriority)
		}
		body["Enable"+b.wire+"SharedVoicemailTranscription"] = b.transcription.ValueBool()
		body["Enable"+b.wire+"SharedVoicemailSystemPromptSuppression"] = action == "shared_voicemail" && b.suppression.ValueBool()
		if action == "shared_voicemail" {
			body[b.wire+"SharedVoicemailTextToSpeechPrompt"] = optStringWire(*b.svPrompt)
			body[b.wire+"SharedVoicemailAudioFilePrompt"] = optStringWire(*b.svAudio)
		} else {
			body[b.wire+"SharedVoicemailTextToSpeechPrompt"] = nil
			body[b.wire+"SharedVoicemailAudioFilePrompt"] = nil
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
	if ids := listStrings(m.HideAuthorizedUsers); ids != nil {
		body["HideAuthorizedUsers"] = ids
	}
	if ids := listStrings(m.OboResourceAccountIDs); ids != nil {
		body["OboResourceAccountIds"] = ids
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
	m.ChannelID = optStringValue(obj["ThreadId"], m.ChannelID)
	if m.OverwriteCallableChannel.IsNull() || m.OverwriteCallableChannel.IsUnknown() {
		m.OverwriteCallableChannel = types.BoolValue(false)
	}
	welcome := optIDValue(obj["WelcomeMusicResourceId"], m.WelcomeAudioFileID)
	if welcome.IsNull() {
		welcome = optIDValue(obj["WelcomeMusicAudioFileId"], m.WelcomeAudioFileID)
	}
	m.WelcomeAudioFileID = welcome
	if useDefault, ok := obj["UseDefaultMusicOnHold"].(bool); ok && useDefault {
		m.MusicOnHoldAudioFileID = types.StringNull()
	} else {
		m.MusicOnHoldAudioFileID = optIDValue(obj["MusicOnHoldResourceId"], m.MusicOnHoldAudioFileID)
	}
	for _, b := range m.branches() {
		diags.Append(readEnum(obj, b.wire+"Action", b.wire+"Action", b.enum, false, b.action)...)
		*b.target = callQueueTargetValue(obj[b.wire+"ActionTarget"], *b.target)
		*b.svPrompt = optStringValue(obj[b.wire+"SharedVoicemailTextToSpeechPrompt"], *b.svPrompt)
		*b.svAudio = optIDValue(obj[b.wire+"SharedVoicemailAudioFilePrompt"], *b.svAudio)
		*b.discPrompt = optStringValue(obj[b.wire+"DisconnectTextToSpeechPrompt"], *b.discPrompt)
		readBool(obj, "Enable"+b.wire+"SharedVoicemailTranscription", b.transcription)
		readBool(obj, "Enable"+b.wire+"SharedVoicemailSystemPromptSuppression", b.suppression)
		if b.suppression.IsNull() || b.suppression.IsUnknown() {
			*b.suppression = types.BoolValue(false)
		}
		*b.discAudio = optIDValue(obj[b.wire+"DisconnectAudioFilePrompt"], *b.discAudio)
		for i, k := range callQueueRedirectKinds {
			*b.redirect[i].tts = optStringValue(obj[b.wire+"Redirect"+k.wire+"TextToSpeechPrompt"], *b.redirect[i].tts)
			*b.redirect[i].audio = optIDValue(obj[b.wire+"Redirect"+k.wire+"AudioFilePrompt"], *b.redirect[i].audio)
		}
		prior := *b.priority
		readInt64(obj, b.wire+"ActionCallPriority", b.priority)
		if prior.IsNull() && !b.priority.IsNull() && b.priority.ValueInt64() == callQueueDefaultCallPriority {
			*b.priority = types.Int64Null()
		}
	}
	readBool(obj, "IsCallbackEnabled", &m.IsCallbackEnabled)
	if m.IsCallbackEnabled.IsNull() || m.IsCallbackEnabled.IsUnknown() {
		m.IsCallbackEnabled = types.BoolValue(false)
	}
	diags.Append(readEnum(obj, "CallbackRequestDtmf", "CallbackRequestDtmf", callbackDtmfKeys, false, &m.CallbackRequestDtmf)...)
	readInt64(obj, "WaitTimeBeforeOfferingCallbackInSecond", &m.WaitTimeBeforeOfferingCallbackInSecond)
	readInt64(obj, "NumberOfCallsInQueueBeforeOfferingCallback", &m.NumberOfCallsInQueueBeforeOfferingCallback)
	readInt64(obj, "CallToAgentRatioThresholdBeforeOfferingCallback", &m.CallToAgentRatioThresholdBeforeOfferingCallback)
	m.CallbackOfferTextToSpeechPrompt = optStringValue(obj["CallbackOfferTextToSpeechPrompt"], m.CallbackOfferTextToSpeechPrompt)
	m.CallbackOfferAudioFileID = optIDValue(obj["CallbackOfferAudioFilePromptResourceId"], m.CallbackOfferAudioFileID)
	m.CallbackEmailNotificationTarget = callQueueTargetValue(obj["CallbackEmailNotificationTarget"], m.CallbackEmailNotificationTarget)
	readInt64(obj, "ServiceLevelThresholdResponseTimeInSecond", &m.ServiceLevelThresholdResponseTimeInSecond)
	m.ShiftsTeamID = optStringValue(obj["ShiftsTeamId"], m.ShiftsTeamID)
	m.ShiftsSchedulingGroupID = optStringValue(obj["ShiftsSchedulingGroupId"], m.ShiftsSchedulingGroupID)
	diags.Append(readEnum(obj, "NoAgentApplyTo", "NoAgentApplyTo", noAgentApplyTos, false, &m.NoAgentApplyTo)...)
	if m.NoAgentApplyTo.IsNull() || m.NoAgentApplyTo.IsUnknown() {
		m.NoAgentApplyTo = types.StringValue("all_calls")
	}
	var d diag.Diagnostics
	m.Users, d = optListValue(keepPriorOrder(callQueueAgentIDs(obj), m.Users), m.Users)
	diags.Append(d...)
	m.DistributionLists, d = optListValue(stringsOf(obj["DistributionLists"]), m.DistributionLists)
	diags.Append(d...)
	m.AuthorizedUsers, d = optListValue(stringsOf(obj["AuthorizedUsers"]), m.AuthorizedUsers)
	diags.Append(d...)
	m.HideAuthorizedUsers, d = optListValue(stringsOf(obj["HideAuthorizedUsers"]), m.HideAuthorizedUsers)
	diags.Append(d...)
	m.ApplicationInstances, d = stringListValue(stringsOf(obj["ApplicationInstances"]))
	diags.Append(d...)
	m.OboResourceAccountIDs, d = optListValue(stringsOf(obj["OboResourceAccountIds"]), m.OboResourceAccountIDs)
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
// getCallQueueWithAgents reads a call queue, re-reading until its agents match the expected ids or the wait runs out.
func getCallQueueWithAgents(ctx context.Context, api *teamsapi.Client, id string, want []string) (map[string]any, error) {
	deadline := time.Now().Add(agentReadWait.timeout)
	for {
		obj, err := getVoiceApp(ctx, api, callQueueKind, id)
		if err != nil || sameIDs(callQueueAgentIDs(obj), want) || time.Now().After(deadline) {
			return obj, err
		}
		select {
		case <-ctx.Done():
			return obj, nil
		case <-time.After(agentReadWait.interval):
		}
	}
}

// agentReadWait bounds how long a write waits for the queue's agents to read back.
var agentReadWait = struct {
	timeout, interval time.Duration
}{timeout: 30 * time.Second, interval: 2 * time.Second}

// sameIDs reports whether two id lists hold the same ids, ignoring order and case.
func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[strings.ToLower(x)]++
	}
	for _, x := range b {
		k := strings.ToLower(x)
		if seen[k] == 0 {
			return false
		}
		seen[k]--
	}
	return true
}

func callQueueAgentIDs(obj map[string]any) []string {
	if ids := agentIDs(obj["Agents"]); len(ids) > 0 {
		return ids
	}
	return stringsOf(obj["Users"])
}

// Call queue callback and no agent enums.
var (
	callbackDtmfKeys = newWireEnum(
		[2]string{"tone0", "Tone0"},
		[2]string{"tone1", "Tone1"},
		[2]string{"tone2", "Tone2"},
		[2]string{"tone3", "Tone3"},
		[2]string{"tone4", "Tone4"},
		[2]string{"tone5", "Tone5"},
		[2]string{"tone6", "Tone6"},
		[2]string{"tone7", "Tone7"},
		[2]string{"tone8", "Tone8"},
		[2]string{"tone9", "Tone9"},
		[2]string{"star", "ToneStar"},
		[2]string{"pound", "TonePound"},
	)
	noAgentApplyTos = newWireEnum(
		[2]string{"all_calls", "AllCalls"},
		[2]string{"new_calls", "NewCalls"},
	)
)

// noAgentApplyToNumber returns the number Teams stores for a no_agent_apply_to word.
func noAgentApplyToNumber(tfWord string) (int, bool) {
	switch tfWord {
	case "all_calls":
		return 0, true
	case "new_calls":
		return 1, true
	}
	return 0, false
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
