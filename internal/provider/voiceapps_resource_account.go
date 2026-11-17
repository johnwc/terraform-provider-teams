package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/terraform-provider-teams/internal/clients"
)

var (
	_ resource.Resource                = &resourceAccountResource{}
	_ resource.ResourceWithConfigure   = &resourceAccountResource{}
	_ resource.ResourceWithImportState = &resourceAccountResource{}
)

type resourceAccountResource struct{ client *clients.Client }

// NewResourceAccountResource manages a Teams resource account (New-CsOnlineApplicationInstance / Get-CsOnlineApplicationInstance; rename and delete via Microsoft Graph).
func NewResourceAccountResource() resource.Resource { return &resourceAccountResource{} }

const (
	resourceAccountReadAttempts = 10
	resourceAccountReadDelay    = 3 * time.Second
)

type resourceAccountModel struct {
	ID                types.String `tfsdk:"id"`
	UserPrincipalName types.String `tfsdk:"user_principal_name"`
	DisplayName       types.String `tfsdk:"display_name"`
	Type              types.String `tfsdk:"type"`
	PhoneNumber       types.String `tfsdk:"phone_number"`
}

func (r *resourceAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_account"
}

func (r *resourceAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Teams resource account via New-CsOnlineApplicationInstance / Get-CsOnlineApplicationInstance. " +
			"Renames and deletes go through Microsoft Graph. Needs the Teams Telephony Administrator and User Administrator roles. " +
			"To receive a phone number the account needs a Teams Phone Resource Account license, which is managed outside this provider (e.g. with azuread or in Microsoft 365).",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Computed: true, Description: "Object id of the resource account.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"user_principal_name": schema.StringAttribute{Required: true, Description: "User principal name of the resource account. Changing it requires replacement.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"display_name":        schema.StringAttribute{Required: true, Description: "Display name of the resource account."},
			"type":                schema.StringAttribute{Required: true, Description: "Resource account type: auto_attendant or call_queue. Changing it requires replacement.", Validators: []validator.String{resourceAccountTypes.stringValidator()}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"phone_number":        schema.StringAttribute{Computed: true, Description: "Phone number assigned to the resource account, without the tel: prefix. Null when none is assigned or Teams does not report it.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *resourceAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*clients.Client)
}

func (r *resourceAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan resourceAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, d := resourceAccountBody(&plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := createVoiceApp(ctx, r.client.API, resourceAccountKind, body)
	if err != nil {
		resp.Diagnostics.AddError(resourceAccountKind.cmdlet("New")+" failed", err.Error())
		return
	}
	if getString(created, "objectId") == "" {
		resp.Diagnostics.AddError(resourceAccountKind.cmdlet("New")+" failed", "the response did not include the resource account objectId")
		return
	}
	id := getString(created, "objectId")
	plan.PhoneNumber = types.StringNull()
	resp.Diagnostics.Append(resourceAccountModelFromObject(created, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := retryWhileProvisioning(ctx, func() error {
		_, err := getVoiceApp(ctx, r.client.API, resourceAccountKind, id)
		return err
	}, isResourceAccountGone); err != nil {
		resp.Diagnostics.AddError(resourceAccountKind.cmdlet("Get")+" failed", "the new resource account did not appear in Teams: "+err.Error())
	}
}

func (r *resourceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state resourceAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	obj, err := getVoiceApp(ctx, r.client.API, resourceAccountKind, state.ID.ValueString())
	if err != nil {
		if isResourceAccountGone(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(resourceAccountKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resourceAccountModelFromObject(obj, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *resourceAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state resourceAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	want := plan.DisplayName.ValueString()
	if want != state.DisplayName.ValueString() {
		if err := graphPatchUser(ctx, r.client, id, map[string]any{"displayName": want}); err != nil {
			resp.Diagnostics.AddError("Microsoft Graph user update failed", err.Error())
			return
		}
	}
	obj, err := r.readDisplayName(ctx, id, want)
	if err != nil {
		resp.Diagnostics.AddError(resourceAccountKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	if getString(obj, "displayName") != want {
		resp.Diagnostics.AddWarning("Display name not yet visible in Teams",
			fmt.Sprintf("Microsoft Graph accepted display_name %q but Teams still reports %q. The next refresh will show the value Teams holds.", want, getString(obj, "displayName")))
		obj["displayName"] = want
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(resourceAccountModelFromObject(obj, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// readDisplayName reads the resource account until it reports the wanted display name or the attempts run out.
func (r *resourceAccountResource) readDisplayName(ctx context.Context, id, want string) (map[string]any, error) {
	obj, err := getVoiceApp(ctx, r.client.API, resourceAccountKind, id)
	for i := 1; i < resourceAccountReadAttempts && err == nil && getString(obj, "displayName") != want; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(resourceAccountReadDelay):
		}
		obj, err = getVoiceApp(ctx, r.client.API, resourceAccountKind, id)
	}
	return obj, err
}

func (r *resourceAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state resourceAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := graphDeleteUser(ctx, r.client, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Microsoft Graph user delete failed", err.Error())
	}
}

func (r *resourceAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// isResourceAccountGone reports whether err is a 404, or a 400/404 containing "not found".
func isResourceAccountGone(err error) bool {
	if err == nil {
		return false
	}
	return isNotFound(err) || isAPIErrorWith(err, []int{http.StatusBadRequest, http.StatusNotFound}, "not found")
}

// resourceAccountAppID returns the Teams application id for a resource account type.
func resourceAccountAppID(typ string) (string, error) {
	id, ok := resourceAccountTypes.toWire(typ)
	if !ok {
		return "", fmt.Errorf("%q is not a known resource account type", typ)
	}
	return id, nil
}

// resourceAccountType returns the resource account type for a Teams application id.
func resourceAccountType(appID string) (string, error) {
	typ, ok := resourceAccountTypes.fromWireWord(appID)
	if !ok {
		return "", fmt.Errorf("application id %q is not an auto attendant or call queue application id", appID)
	}
	return typ, nil
}

// resourceAccountBody builds the create body from the model.
func resourceAccountBody(m *resourceAccountModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	appID, err := resourceAccountAppID(m.Type.ValueString())
	if err != nil {
		diags.AddAttributeError(path.Root("type"), "Invalid type", err.Error())
		return nil, diags
	}
	return map[string]any{
		"userPrincipalName": m.UserPrincipalName.ValueString(),
		"applicationId":     appID,
		"displayName":       m.DisplayName.ValueString(),
	}, diags
}

// resourceAccountModelFromObject maps a stored application instance onto the model.
func resourceAccountModelFromObject(obj map[string]any, m *resourceAccountModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if id := getString(obj, "objectId"); id != "" {
		m.ID = types.StringValue(id)
	}
	if upn := getString(obj, "userPrincipalName"); upn != "" {
		if m.UserPrincipalName.IsNull() || m.UserPrincipalName.IsUnknown() || !strings.EqualFold(m.UserPrincipalName.ValueString(), upn) {
			m.UserPrincipalName = types.StringValue(upn)
		}
	}
	readString(obj, "displayName", &m.DisplayName)
	if appID := getString(obj, "applicationId"); appID != "" {
		typ, err := resourceAccountType(appID)
		if err != nil {
			diags.AddError("Unexpected applicationId value", err.Error())
			return diags
		}
		m.Type = types.StringValue(typ)
	}
	m.PhoneNumber = resourceAccountPhoneValue(obj["phoneNumber"])
	return diags
}

// resourceAccountPhoneValue maps a read-back phone number to the value without the tel: prefix.
func resourceAccountPhoneValue(v any) types.String {
	s, _ := v.(string)
	s = strings.TrimSpace(s)
	if len(s) >= 4 && strings.EqualFold(s[:4], "tel:") {
		s = s[4:]
	}
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
