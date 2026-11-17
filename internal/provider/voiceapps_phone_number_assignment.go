package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

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
	_ resource.Resource                = &phoneNumberAssignmentResource{}
	_ resource.ResourceWithConfigure   = &phoneNumberAssignmentResource{}
	_ resource.ResourceWithImportState = &phoneNumberAssignmentResource{}
)

type phoneNumberAssignmentResource struct{ client *clients.Client }

// NewPhoneNumberAssignmentResource manages a phone number assigned to a user or resource account (Set-CsPhoneNumberAssignment / Get-CsPhoneNumberAssignment / Remove-CsPhoneNumberAssignment).
func NewPhoneNumberAssignmentResource() resource.Resource { return &phoneNumberAssignmentResource{} }

var e164Pattern = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)

type phoneNumberAssignmentModel struct {
	ID              types.String `tfsdk:"id"`
	Identity        types.String `tfsdk:"identity"`
	PhoneNumber     types.String `tfsdk:"phone_number"`
	PhoneNumberType types.String `tfsdk:"phone_number_type"`
	LocationID      types.String `tfsdk:"location_id"`
}

func (r *phoneNumberAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_phone_number_assignment"
}

func (r *phoneNumberAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Assigns a Teams phone number to a user or resource account. " +
			"The target needs the matching license: for a resource account a Teams Phone Resource Account license, plus a calling plan or Communications Credits for calling_plan numbers. Any change requires replacement.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Computed: true, Description: "<identity>/<phone_number>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"identity":          schema.StringAttribute{Required: true, Description: "Object id of the resource account or user.", PlanModifiers: replace},
			"phone_number":      schema.StringAttribute{Required: true, Description: "Phone number in E.164 form, e.g. +18005550100.", Validators: []validator.String{e164Validator{}}, PlanModifiers: replace},
			"phone_number_type": schema.StringAttribute{Required: true, Description: "Number type: calling_plan, direct_routing or operator_connect.", Validators: []validator.String{phoneNumberTypes.stringValidator()}, PlanModifiers: replace},
			"location_id": schema.StringAttribute{
				Optional: true, Computed: true,
				Description:   "Emergency location id for the number. When unset, the location Teams reports is kept.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *phoneNumberAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*clients.Client)
}

func (r *phoneNumberAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan phoneNumberAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	wireType, d := phoneNumberWireType(plan.PhoneNumberType)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	location := ""
	if !plan.LocationID.IsNull() && !plan.LocationID.IsUnknown() {
		location = plan.LocationID.ValueString()
	}
	identity, number := plan.Identity.ValueString(), plan.PhoneNumber.ValueString()
	op := phoneNumberAssignmentKind.op("Set", http.MethodPost, phoneAssignPath(number, wireType, location))
	if _, err := r.client.API.Invoke(ctx, op, map[string]any{"Identity": identity}); err != nil {
		resp.Diagnostics.AddError(op.CmdletName+" failed", err.Error())
		return
	}
	plan.ID = types.StringValue(phoneAssignmentID(identity, number))
	if plan.LocationID.IsUnknown() {
		plan.LocationID = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *phoneNumberAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state phoneNumberAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	op := phoneNumberAssignmentKind.op("Get", http.MethodGet, phoneAssignmentsPath(state.PhoneNumber.ValueString()))
	res, err := r.client.API.Invoke(ctx, op, nil)
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(op.CmdletName+" failed", err.Error())
		return
	}
	obj := matchPhoneAssignment(res.Value, state.PhoneNumber.ValueString(), state.Identity.ValueString())
	if obj == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(phoneAssignmentModelFromObject(obj, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update stores the plan without calling Teams.
func (r *phoneNumberAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state phoneNumberAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	if plan.LocationID.IsUnknown() {
		plan.LocationID = state.LocationID
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete unassigns the number with a POST to the unassignment route.
func (r *phoneNumberAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state phoneNumberAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	wireType, d := phoneNumberWireType(state.PhoneNumberType)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	op := phoneNumberAssignmentKind.op("Remove", http.MethodPost, phoneUnassignPath(state.PhoneNumber.ValueString(), wireType))
	if _, err := r.client.API.Invoke(ctx, op, map[string]any{"Identity": state.Identity.ValueString()}); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError(op.CmdletName+" failed", err.Error())
	}
}

func (r *phoneNumberAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	identity, number, err := parsePhoneAssignmentID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), phoneAssignmentID(identity, number))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("identity"), identity)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("phone_number"), number)...)
}

func phoneNumberWireType(v types.String) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	w, ok := phoneNumberTypes.toWire(v.ValueString())
	if !ok {
		diags.AddAttributeError(path.Root("phone_number_type"), "Invalid phone_number_type", fmt.Sprintf("%q is not a known phone number type.", v.ValueString()))
	}
	return w, diags
}

// phoneAssignmentID returns the resource id "<identity>/<phone_number>".
func phoneAssignmentID(identity, number string) string { return identity + "/" + number }

// parsePhoneAssignmentID splits an import id "<identity>/<phone_number>".
func parsePhoneAssignmentID(id string) (identity, number string, err error) {
	identity, number, ok := strings.Cut(id, "/")
	if !ok || identity == "" || !e164Pattern.MatchString(number) {
		return "", "", fmt.Errorf("import id %q must be <identity>/<phone_number>, with the phone number in E.164 form, e.g. 00000000-0000-0000-0000-000000000000/+18005550100", id)
	}
	return identity, number, nil
}

// phoneAssignPath returns the assignment route with an {identity} placeholder.
func phoneAssignPath(number, wireType, locationID string) string {
	p := tnmPathPrefix + "cmdlet/assignment?identity={identity}" +
		"&phoneNumber=" + url.QueryEscape(number) +
		"&phoneNumberType=" + url.QueryEscape(wireType) +
		"&assignmentCategory=Primary"
	if locationID != "" {
		p += "&locationId=" + url.QueryEscape(locationID)
	}
	return p
}

// phoneAssignmentsPath returns the route listing the assignments of a number.
func phoneAssignmentsPath(number string) string {
	return tnmPathPrefix + "cmdlet/assignments?telephoneNumber=" + url.QueryEscape(number)
}

// phoneUnassignPath returns the unassignment route; {identity} is substituted by the transport.
func phoneUnassignPath(number, wireType string) string {
	return tnmPathPrefix + "cmdlet/unassignment/user/{identity}?phoneNumber=" + url.QueryEscape(number) +
		"&phoneNumberType=" + url.QueryEscape(wireType)
}

// normalizePhone strips a tel: prefix and a leading "+" for comparison.
func normalizePhone(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 4 && strings.EqualFold(s[:4], "tel:") {
		s = s[4:]
	}
	return strings.TrimPrefix(s, "+")
}

// phoneAssignmentList returns the assignment objects of a read response.
func phoneAssignmentList(v []map[string]any) []map[string]any {
	if len(v) == 1 {
		if items, ok := v[0]["value"].([]any); ok {
			out := make([]map[string]any, 0, len(items))
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					out = append(out, m)
				}
			}
			return out
		}
	}
	return v
}

// matchPhoneAssignment returns the assignment of number to identity, or nil.
func matchPhoneAssignment(v []map[string]any, number, identity string) map[string]any {
	want := normalizePhone(number)
	for _, obj := range phoneAssignmentList(v) {
		if normalizePhone(getString(obj, "TelephoneNumber")) != want {
			continue
		}
		target := getString(obj, "AssignedPstnTargetId")
		if target != "" && strings.EqualFold(target, identity) {
			return obj
		}
	}
	return nil
}

// phoneAssignmentModelFromObject maps an assignment object onto the model.
func phoneAssignmentModelFromObject(obj map[string]any, m *phoneNumberAssignmentModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if v := getString(obj, "NumberType"); v != "" {
		typ, ok := phoneNumberTypes.fromWireWord(v)
		if !ok {
			diags.AddError("Unexpected NumberType value", fmt.Sprintf("Teams returned NumberType = %v, which this provider does not recognise.", v))
			return diags
		}
		m.PhoneNumberType = types.StringValue(typ)
	}
	if loc := getString(obj, "LocationId"); loc != "" {
		m.LocationID = keepCase(loc, m.LocationID)
	} else if m.LocationID.IsUnknown() {
		m.LocationID = types.StringNull()
	}
	m.ID = types.StringValue(phoneAssignmentID(m.Identity.ValueString(), m.PhoneNumber.ValueString()))
	return diags
}

// e164Validator checks that a string is an E.164 phone number such as +18005550100.
type e164Validator struct{}

func (v e164Validator) Description(_ context.Context) string {
	return "value must be an E.164 phone number such as +18005550100"
}

func (v e164Validator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v e164Validator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if s := req.ConfigValue.ValueString(); !e164Pattern.MatchString(s) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid attribute value", fmt.Sprintf("%q is not valid; %s.", s, v.Description(ctx)))
	}
}
