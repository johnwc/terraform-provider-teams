package provider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
	"github.com/terraprovider/terraform-provider-teams/internal/clients"
)

var (
	_ resource.Resource                   = &audioFileResource{}
	_ resource.ResourceWithConfigure      = &audioFileResource{}
	_ resource.ResourceWithImportState    = &audioFileResource{}
	_ resource.ResourceWithValidateConfig = &audioFileResource{}
	_ resource.ResourceWithModifyPlan     = &audioFileResource{}
)

type audioFileResource struct{ client *clients.Client }

// NewAudioFileResource manages a Teams audio file (Import-CsOnlineAudioFile / Get-CsOnlineAudioFile / Remove-CsOnlineAudioFile).
func NewAudioFileResource() resource.Resource { return &audioFileResource{} }

const (
	// audioFileBasePath is the upload path; files live under "/<ApplicationId>/<Id>".
	audioFileBasePath = "/Teams.MediaStorage/audiofile"
	// audioFileMaxBytes is the largest file Teams accepts.
	audioFileMaxBytes = 5 * 1024 * 1024
	// audioFileUnlinkedWarning is the delete warning for a file Teams removes itself.
	audioFileUnlinkedWarning = "Teams removes unlinked audio files on its own within 72 hours."
)

// audioFileExtensions are the accepted file name extensions.
var audioFileExtensions = []string{".wav", ".mp3", ".wma"}

// audioFileKind returns the voice app kind for the audio files of one application id.
func audioFileKind(appID string) voiceAppKind {
	return voiceAppKind{noun: "OnlineAudioFile", path: audioFileBasePath + "/" + url.PathEscape(appID), label: "audio file"}
}

type audioFileModel struct {
	ID                   types.String `tfsdk:"id"`
	Usage                types.String `tfsdk:"usage"`
	Source               types.String `tfsdk:"source"`
	FileName             types.String `tfsdk:"file_name"`
	SourceSHA256         types.String `tfsdk:"source_sha256"`
	Duration             types.String `tfsdk:"duration"`
	ScheduledForDeletion types.Bool   `tfsdk:"scheduled_for_deletion"`
}

func (r *audioFileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_audio_file"
}

func (r *audioFileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Uploads a recording for auto attendant menus and call queues. " +
			"Teams deletes a file that no menu or queue uses within about 48 hours of upload, so link it in the same apply. " +
			"The file content is not stored in state. Any change requires replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Audio file id.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"usage": schema.StringAttribute{
				Required: true, Description: "Where the file is used: auto_attendant, call_queue or tenant_global.",
				Validators: []validator.String{audioFileUsages.stringValidator()}, PlanModifiers: replace,
			},
			"source": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				Description: "Path of the local file to upload, read at plan and apply time. WAV, WMA or MP3, at most 5 MB. " +
					"After an import this is unset, so setting it replaces the file.",
			},
			"file_name": schema.StringAttribute{
				Optional: true, Computed: true,
				Description:   "File name stored in Teams, ending in .wav, .mp3 or .wma. Defaults to the base name of source.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()},
			},
			"source_sha256":          schema.StringAttribute{Computed: true, Description: "SHA-256 of the uploaded file. A changed file is uploaded again."},
			"duration":               schema.StringAttribute{Computed: true, Description: "Length of the recording, e.g. 00:00:03."},
			"scheduled_for_deletion": schema.BoolAttribute{Computed: true, Description: "True while no auto attendant or call queue uses the file and Teams plans to delete it."},
		},
	}
}

func (r *audioFileResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*clients.Client)
}

func (r *audioFileResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg audioFileModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateAudioFile(&cfg)...)
}

// validateAudioFile checks the extension of the stored file name.
func validateAudioFile(m *audioFileModel) diag.Diagnostics {
	var diags diag.Diagnostics
	attr, name := "file_name", ""
	switch {
	case m.FileName.IsUnknown():
		return diags
	case !m.FileName.IsNull():
		name = m.FileName.ValueString()
	case !m.Source.IsNull() && !m.Source.IsUnknown():
		attr, name = "source", filepath.Base(m.Source.ValueString())
	default:
		return diags
	}
	if err := checkAudioFileName(name); err != nil {
		diags.AddAttributeError(path.Root(attr), "Invalid audio file", err.Error())
	}
	return diags
}

// ModifyPlan sets the default file name and the source hash, and replaces the file when the hash changes.
func (r *audioFileResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan audioFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state := audioFileModel{FileName: types.StringNull(), SourceSHA256: types.StringNull()}
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if plan.Source.IsNull() || plan.Source.IsUnknown() {
		return
	}
	src := plan.Source.ValueString()

	var cfgName types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("file_name"), &cfgName)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfgName.IsNull() {
		name := types.StringValue(filepath.Base(src))
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("file_name"), name)...)
		if !req.State.Raw.IsNull() && !state.FileName.IsNull() && !state.FileName.Equal(name) {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("file_name"))
		}
	}

	_, sum, err := readAudioSource(src)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("source"), "Cannot read audio file", err.Error())
		return
	}
	planned, replace := audioFileHashPlan(state.SourceSHA256, sum)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("source_sha256"), planned)...)
	if replace {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("source_sha256"))
	}
}

// audioFileHashPlan returns the planned source_sha256 and whether it differs from a known prior hash.
func audioFileHashPlan(prior types.String, sum string) (types.String, bool) {
	replace := !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() != sum
	return types.StringValue(sum), replace
}

func (r *audioFileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan audioFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	appID, ok := audioFileUsages.toWire(plan.Usage.ValueString())
	if !ok {
		resp.Diagnostics.AddAttributeError(path.Root("usage"), "Invalid usage", fmt.Sprintf("%q is not a known usage.", plan.Usage.ValueString()))
		return
	}
	src := plan.Source.ValueString()
	content, sum, err := readAudioSource(src)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("source"), "Cannot read audio file", err.Error())
		return
	}
	if !plan.SourceSHA256.IsNull() && !plan.SourceSHA256.IsUnknown() && plan.SourceSHA256.ValueString() != sum {
		resp.Diagnostics.AddAttributeError(path.Root("source"), "Audio file changed", fmt.Sprintf("%s changed between plan and apply; run the plan again.", src))
		return
	}
	name := filepath.Base(src)
	if !plan.FileName.IsNull() && !plan.FileName.IsUnknown() {
		name = plan.FileName.ValueString()
	}
	if err := checkAudioFileName(name); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("file_name"), "Invalid audio file", err.Error())
		return
	}
	k := audioFileKind(appID)
	obj, err := uploadAudioFile(ctx, r.client.API, appID, name, content)
	if err != nil {
		resp.Diagnostics.AddError(k.cmdlet("Import")+" failed", err.Error())
		return
	}
	plan.FileName = types.StringValue(name)
	plan.SourceSHA256 = types.StringValue(sum)
	plan.ID = types.StringNull()
	resp.Diagnostics.Append(audioFileModelFromObject(obj, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.ID.IsNull() {
		resp.Diagnostics.AddError(k.cmdlet("Import")+" failed", "the response did not include the audio file id")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *audioFileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state audioFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	appID, ok := audioFileUsages.toWire(state.Usage.ValueString())
	if !ok {
		resp.Diagnostics.AddAttributeError(path.Root("usage"), "Invalid usage", fmt.Sprintf("%q is not a known usage.", state.Usage.ValueString()))
		return
	}
	obj, gone, err := getAudioFile(ctx, r.client.API, appID, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(audioFileKind(appID).cmdlet("Get")+" failed", err.Error())
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(audioFileModelFromObject(obj, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update re-reads the file and stores the plan; every configurable change replaces the file.
func (r *audioFileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state audioFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	plan.Duration = state.Duration
	plan.ScheduledForDeletion = state.ScheduledForDeletion
	if plan.FileName.IsUnknown() {
		plan.FileName = state.FileName
	}
	if plan.SourceSHA256.IsUnknown() {
		plan.SourceSHA256 = state.SourceSHA256
	}
	if appID, ok := audioFileUsages.toWire(plan.Usage.ValueString()); ok {
		obj, gone, err := getAudioFile(ctx, r.client.API, appID, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(audioFileKind(appID).cmdlet("Get")+" failed", err.Error())
			return
		}
		if !gone {
			resp.Diagnostics.Append(audioFileModelFromObject(obj, &plan)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *audioFileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state audioFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	appID, ok := audioFileUsages.toWire(state.Usage.ValueString())
	if !ok {
		resp.Diagnostics.AddAttributeError(path.Root("usage"), "Invalid usage", fmt.Sprintf("%q is not a known usage.", state.Usage.ValueString()))
		return
	}
	resp.Diagnostics.Append(deleteAudioFile(ctx, r.client.API, appID, state.ID.ValueString())...)
}

func (r *audioFileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	usage, id, err := parseAudioFileImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("usage"), usage)...)
}

// parseAudioFileImportID splits an import id "<usage>/<id>".
func parseAudioFileImportID(importID string) (usage, id string, err error) {
	usage, id, ok := strings.Cut(importID, "/")
	if _, known := audioFileUsages.toWire(usage); !ok || !known || id == "" || strings.Contains(id, "/") {
		return "", "", fmt.Errorf("import id %q must be <usage>/<id>, with usage one of %s, e.g. auto_attendant/8bc2a0f1d6e94c2b9f3e7a1d5c4b2e10",
			importID, strings.Join(audioFileUsages.tfWords, ", "))
	}
	return usage, id, nil
}

// checkAudioFileName checks that name ends in a supported extension.
func checkAudioFileName(name string) error {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range audioFileExtensions {
		if ext == e {
			return nil
		}
	}
	return fmt.Errorf("%q must end in one of %s", name, strings.Join(audioFileExtensions, ", "))
}

// checkAudioFileSize checks that a file of n bytes is within the Teams limit.
func checkAudioFileSize(n int64) error {
	if n > audioFileMaxBytes {
		return fmt.Errorf("the file is %d bytes; the limit is %d bytes (5 MB)", n, audioFileMaxBytes)
	}
	return nil
}

// readAudioSource reads the file at p and returns its content and hex SHA-256.
func readAudioSource(p string) ([]byte, string, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, "", err
	}
	if fi.IsDir() {
		return nil, "", fmt.Errorf("%s is a directory", p)
	}
	if err := checkAudioFileSize(fi.Size()); err != nil {
		return nil, "", fmt.Errorf("%s: %w", p, err)
	}
	content, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	if err := checkAudioFileSize(int64(len(content))); err != nil {
		return nil, "", fmt.Errorf("%s: %w", p, err)
	}
	sum := sha256.Sum256(content)
	return content, hex.EncodeToString(sum[:]), nil
}

// audioFileUploadBody builds the upload body.
func audioFileUploadBody(appID, fileName string, content []byte) map[string]any {
	return map[string]any{
		"ApplicationId":    appID,
		"OriginalFilename": fileName,
		"Content":          base64.StdEncoding.EncodeToString(content),
	}
}

// uploadAudioFile uploads a file and returns the stored file object.
func uploadAudioFile(ctx context.Context, api voiceAppsInvoker, appID, fileName string, content []byte) (map[string]any, error) {
	k := audioFileKind(appID)
	res, err := api.Invoke(ctx, k.op("Import", http.MethodPost, audioFileBasePath), audioFileUploadBody(appID, fileName, content))
	if err != nil {
		return nil, err
	}
	obj := k.unwrap(res.Value)
	if obj == nil {
		return nil, fmt.Errorf("no %s object in the %s response", k.label, k.cmdlet("Import"))
	}
	return obj, nil
}

// getAudioFile reads one audio file. gone reports a 404, or a 400 for an id the list does not contain.
func getAudioFile(ctx context.Context, api voiceAppsInvoker, appID, id string) (map[string]any, bool, error) {
	k := audioFileKind(appID)
	obj, err := getVoiceApp(ctx, api, k, id)
	if err == nil {
		return obj, false, nil
	}
	if isNotFound(err) {
		return nil, true, nil
	}
	if s := apiErrorStatus(err); s != http.StatusBadRequest && s != http.StatusNotFound {
		return nil, false, err
	}
	res, lerr := api.Invoke(ctx, k.op("Get", http.MethodGet, k.path), nil)
	if lerr != nil {
		return nil, false, err
	}
	for _, it := range audioFileList(res.Value) {
		if strings.EqualFold(getString(it, "Id"), id) {
			return nil, false, err
		}
	}
	return nil, true, nil
}

// audioFileList returns the file objects of a list response.
func audioFileList(v []map[string]any) []map[string]any {
	if len(v) == 1 {
		if items, ok := v[0]["value"].([]any); ok {
			return objectsOf(items)
		}
	}
	return v
}

// deleteAudioFile removes a file. A failed delete of a file that is gone or
// scheduled for deletion succeeds with a warning.
func deleteAudioFile(ctx context.Context, api voiceAppsInvoker, appID, id string) diag.Diagnostics {
	var diags diag.Diagnostics
	k := audioFileKind(appID)
	derr := deleteVoiceApp(ctx, api, k, id)
	if derr == nil {
		return diags
	}
	obj, gone, err := getAudioFile(ctx, api, appID, id)
	if err == nil && (gone || audioFileScheduled(obj)) {
		diags.AddWarning("Audio file left for Teams to remove", audioFileUnlinkedWarning)
		return diags
	}
	diags.AddError(k.cmdlet("Remove")+" failed", derr.Error())
	return diags
}

// audioFileScheduled reports whether DeletionTimestampOffset is set.
func audioFileScheduled(obj map[string]any) bool {
	v, ok := obj["DeletionTimestampOffset"]
	if !ok || v == nil {
		return false
	}
	s, isString := v.(string)
	return !isString || s != ""
}

// apiErrorStatus returns the HTTP status of an APIError, or 0.
func apiErrorStatus(err error) int {
	var ae *teamsapi.APIError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

// audioFileModelFromObject maps a stored audio file object onto the model.
func audioFileModelFromObject(obj map[string]any, m *audioFileModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if id := getString(obj, "Id"); id != "" {
		m.ID = keepCase(id, m.ID)
	}
	diags.Append(readEnum(obj, "ApplicationId", "ApplicationId", audioFileUsages, true, &m.Usage)...)
	if name := getString(obj, "OriginalFilename"); name != "" {
		m.FileName = keepCase(name, m.FileName)
	} else if m.FileName.IsUnknown() {
		m.FileName = types.StringNull()
	}
	if d := getString(obj, "Duration"); d != "" {
		m.Duration = types.StringValue(d)
	} else {
		m.Duration = types.StringNull()
	}
	m.ScheduledForDeletion = types.BoolValue(audioFileScheduled(obj))
	if m.SourceSHA256.IsUnknown() {
		m.SourceSHA256 = types.StringNull()
	}
	return diags
}

// optIDValue maps a nullable read-back id, keeping a prior value that differs only in case.
func optIDValue(v any, prior types.String) types.String {
	s, ok := v.(string)
	if !ok || s == "" {
		return types.StringNull()
	}
	return keepCase(s, prior)
}
