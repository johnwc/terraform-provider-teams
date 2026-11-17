package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/terraform-provider-teams/internal/clients"
)

var (
	_ resource.Resource                = &resourceAccountAssociationResource{}
	_ resource.ResourceWithConfigure   = &resourceAccountAssociationResource{}
	_ resource.ResourceWithImportState = &resourceAccountAssociationResource{}
)

type resourceAccountAssociationResource struct{ client *clients.Client }

// NewResourceAccountAssociationResource manages the link between a resource account and an auto attendant or call queue (New-CsOnlineApplicationInstanceAssociation / Get-CsOnlineApplicationInstanceAssociation / Remove-CsOnlineApplicationInstanceAssociation).
func NewResourceAccountAssociationResource() resource.Resource {
	return &resourceAccountAssociationResource{}
}

// associationNotFoundCode is the delete ErrorCode Teams returns when there is no association.
const associationNotFoundCode = "RemoveApplicationInstanceAssociation_AssociationNotFound"

type resourceAccountAssociationModel struct {
	ID                types.String `tfsdk:"id"`
	ResourceAccountID types.String `tfsdk:"resource_account_id"`
	ConfigurationID   types.String `tfsdk:"configuration_id"`
	ConfigurationType types.String `tfsdk:"configuration_type"`
	CallPriority      types.Int64  `tfsdk:"call_priority"`
}

func (r *resourceAccountAssociationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_account_association"
}

func (r *resourceAccountAssociationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Links a Teams resource account to an auto attendant or call queue, so calls to the account's number reach it. Any change requires replacement.",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Computed: true, Description: "Object id of the resource account.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"resource_account_id": schema.StringAttribute{Required: true, Description: "Object id of the resource account.", PlanModifiers: replace},
			"configuration_id":    schema.StringAttribute{Required: true, Description: "Id of the auto attendant or call queue.", PlanModifiers: replace},
			"configuration_type":  schema.StringAttribute{Required: true, Description: "Type of the configuration: auto_attendant or call_queue.", Validators: []validator.String{associationConfigTypes.stringValidator()}, PlanModifiers: replace},
			"call_priority": schema.Int64Attribute{
				Optional: true, Computed: true, Default: int64default.StaticInt64(3),
				Description:   "Priority of calls through this resource account (1-5, 1 is highest).",
				Validators:    []validator.Int64{int64Range{lo: 1, hi: 5}},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *resourceAccountAssociationResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*clients.Client)
}

func (r *resourceAccountAssociationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan resourceAccountAssociationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, d := associationBody(&plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	cmdlet := associationKind.cmdlet("New")
	var assoc map[string]any
	err := retryWhileProvisioning(ctx, func() error {
		res, err := r.client.API.Invoke(ctx, associationKind.op("New", http.MethodPost, associationKind.path), body)
		if err != nil {
			return err
		}
		assoc, err = parseAssociationCreate(res.Value)
		return err
	}, isEndpointProvisioning)
	if err != nil {
		resp.Diagnostics.AddError(cmdlet+" failed", err.Error())
		return
	}
	plan.ID = plan.ResourceAccountID
	if assoc != nil {
		resp.Diagnostics.Append(associationModelFromObject(assoc, &plan)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *resourceAccountAssociationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state resourceAccountAssociationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.client.API.Invoke(ctx, associationKind.op("Get", http.MethodGet, associationKind.itemPath()), map[string]any{"Identity": state.ID.ValueString()})
	if err != nil {
		if isAssociationGone(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(associationKind.cmdlet("Get")+" failed", err.Error())
		return
	}
	assoc := unwrapAssociation(res.Value)
	if assoc == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(associationModelFromObject(assoc, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update stores the plan without calling Teams.
func (r *resourceAccountAssociationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state resourceAccountAssociationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *resourceAccountAssociationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state resourceAccountAssociationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cmdlet := associationKind.cmdlet("Remove")
	res, err := r.client.API.Invoke(ctx, associationKind.op("Remove", http.MethodDelete, associationKind.itemPath()), map[string]any{"Identity": state.ID.ValueString()})
	if err != nil {
		if !isAssociationGone(err) {
			resp.Diagnostics.AddError(cmdlet+" failed", err.Error())
		}
		return
	}
	if err := parseAssociationDelete(res.Value); err != nil {
		resp.Diagnostics.AddError(cmdlet+" failed", err.Error())
	}
}

func (r *resourceAccountAssociationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// isAssociationGone reports whether err is a 404, or a 400/404 containing "Association not found".
func isAssociationGone(err error) bool {
	if err == nil {
		return false
	}
	return isNotFound(err) || isAPIErrorWith(err, []int{http.StatusBadRequest, http.StatusNotFound}, "Association not found")
}

// associationBody builds the create body from the model.
func associationBody(m *resourceAccountAssociationModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	typ, ok := associationConfigTypes.toWire(m.ConfigurationType.ValueString())
	if !ok {
		diags.AddAttributeError(path.Root("configuration_type"), "Invalid configuration_type", fmt.Sprintf("%q is not a known configuration type.", m.ConfigurationType.ValueString()))
		return nil, diags
	}
	return map[string]any{
		"EndpointsIds":      []string{m.ResourceAccountID.ValueString()},
		"ConfigurationId":   m.ConfigurationID.ValueString(),
		"ConfigurationType": typ,
		"CallPriority":      m.CallPriority.ValueInt64(),
	}, diags
}

// associationEndpointNotFoundCode is the create ErrorCode returned while the resource account is not yet available.
const associationEndpointNotFoundCode = "CreateApplicationInstanceAssociation_AppEndpointNotFound"

// associationError is a failure reported in the StatusRecord of an association result.
type associationError struct{ Code, Message string }

func (e *associationError) Error() string { return fmt.Sprintf("%s (%s)", e.Message, e.Code) }

// isEndpointProvisioning reports whether err is the endpoint-not-found association error.
func isEndpointProvisioning(err error) bool {
	var ae *associationError
	return errors.As(err, &ae) && ae.Code == associationEndpointNotFoundCode
}

// associationResults returns the AssociationOperationResults entries of a write response.
func associationResults(v []map[string]any) []map[string]any {
	items, _ := firstObject(v)["AssociationOperationResults"].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// associationStatus returns the StatusRecord error code and message of one result.
func associationStatus(result map[string]any) (code, message string) {
	sr, _ := result["StatusRecord"].(map[string]any)
	return getString(sr, "ErrorCode"), getString(sr, "Message")
}

// parseAssociationCreate checks a create response and returns the first
// ApplicationInstanceAssociation object, which may be nil.
func parseAssociationCreate(v []map[string]any) (map[string]any, error) {
	results := associationResults(v)
	if len(results) == 0 {
		return nil, fmt.Errorf("no AssociationOperationResults in the %s response", associationKind.cmdlet("New"))
	}
	for _, res := range results {
		if code, msg := associationStatus(res); code != "" {
			return nil, &associationError{Code: code, Message: firstNonEmptyStr(msg, "the association failed")}
		}
	}
	assoc, _ := results[0]["ApplicationInstanceAssociation"].(map[string]any)
	return assoc, nil
}

// parseAssociationDelete checks a delete response. A missing association counts as deleted.
func parseAssociationDelete(v []map[string]any) error {
	for _, res := range associationResults(v) {
		if code, msg := associationStatus(res); code != "" && !strings.EqualFold(code, associationNotFoundCode) {
			return fmt.Errorf("%s (%s)", firstNonEmptyStr(msg, "the association removal failed"), code)
		}
	}
	return nil
}

// unwrapAssociation returns the ApplicationInstanceAssociation object of a read response.
func unwrapAssociation(v []map[string]any) map[string]any {
	assoc, _ := firstObject(v)["ApplicationInstanceAssociation"].(map[string]any)
	return assoc
}

// associationModelFromObject maps a stored association onto the model.
func associationModelFromObject(obj map[string]any, m *resourceAccountAssociationModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if id := getString(obj, "Id"); id != "" {
		m.ID = keepCase(id, m.ID)
		m.ResourceAccountID = keepCase(id, m.ResourceAccountID)
	}
	if id := getString(obj, "ConfigurationId"); id != "" {
		m.ConfigurationID = keepCase(id, m.ConfigurationID)
	}
	if v := firstNonEmptyStr(getString(obj, "ApplicationConfigurationType"), getString(obj, "ConfigurationType")); v != "" {
		typ, ok := associationConfigTypes.fromWireWord(v)
		if !ok {
			diags.AddError("Unexpected ApplicationConfigurationType value", fmt.Sprintf("Teams returned ApplicationConfigurationType = %v, which this provider does not recognise.", v))
			return diags
		}
		m.ConfigurationType = types.StringValue(typ)
	}
	readInt64(obj, "CallPriority", &m.CallPriority)
	return diags
}

// keepCase returns prior when it equals s ignoring case, otherwise s.
func keepCase(s string, prior types.String) types.String {
	if !prior.IsNull() && !prior.IsUnknown() && strings.EqualFold(prior.ValueString(), s) {
		return prior
	}
	return types.StringValue(s)
}
