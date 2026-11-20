package provider

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
)

// voiceAppKind describes one voice app collection on the config API.
type voiceAppKind struct {
	noun        string // cmdlet noun after "-Cs", e.g. "CallQueue"
	path        string // collection path; the item path appends "/{identity}"
	wrapper     string // response wrapper key, e.g. "CallQueue"
	listWrapper string // plural response wrapper key; empty when not used
	label       string // human-readable name used in errors
}

var (
	callQueueKind = voiceAppKind{
		noun: "CallQueue", path: "/Teams.VoiceApps/callqueues",
		wrapper: "CallQueue", listWrapper: "CallQueues", label: "call queue",
	}
	scheduleKind = voiceAppKind{
		noun: "OnlineSchedule", path: "/Teams.VoiceApps/schedules",
		wrapper: "Schedule", label: "schedule",
	}
	autoAttendantKind = voiceAppKind{
		noun: "AutoAttendant", path: "/Teams.VoiceApps/auto-attendants",
		wrapper: "AutoAttendant", label: "auto attendant",
	}
	resourceAccountKind = voiceAppKind{
		noun: "OnlineApplicationInstance", path: "/Teams.PlatformService/v2/ApplicationInstances",
		label: "resource account",
	}
	associationKind = voiceAppKind{
		noun: "OnlineApplicationInstanceAssociation", path: "/Teams.VoiceApps/applicationinstanceassociations",
		label: "resource account association",
	}
	phoneNumberAssignmentKind = voiceAppKind{
		noun: "PhoneNumberAssignment", path: tnmPathPrefix + "cmdlet",
		label: "phone number assignment",
	}
)

// voiceAppsInvoker is the subset of *teamsapi.Client used by the voice app helpers.
type voiceAppsInvoker interface {
	Invoke(ctx context.Context, op teamsapi.Op, params map[string]any) (*teamsapi.Result, error)
}

// cmdlet returns the PowerShell cmdlet name for a verb, e.g. "Get-CsCallQueue".
func (k voiceAppKind) cmdlet(verb string) string { return verb + "-Cs" + k.noun }

func (k voiceAppKind) itemPath() string { return k.path + "/{identity}" }

func (k voiceAppKind) op(verb, method, path string) teamsapi.Op {
	return teamsapi.Op{
		CmdletName: k.cmdlet(verb),
		Transport:  teamsapi.ConfigAPI,
		Kind:       teamsapi.AutoRest,
		Method:     method,
		Path:       path,
	}
}

// createVoiceApp posts a new object to the collection and returns the stored object.
func createVoiceApp(ctx context.Context, api voiceAppsInvoker, k voiceAppKind, body map[string]any) (map[string]any, error) {
	return createVoiceAppWithQuery(ctx, api, k, "", body)
}

// createVoiceAppWithQuery creates an object, appending query to the request path.
func createVoiceAppWithQuery(ctx context.Context, api voiceAppsInvoker, k voiceAppKind, query string, body map[string]any) (map[string]any, error) {
	res, err := api.Invoke(ctx, k.op("New", http.MethodPost, k.path+query), body)
	if err != nil {
		return nil, err
	}
	obj := k.unwrap(res.Value)
	if obj == nil {
		return nil, fmt.Errorf("no %s object in the %s response", k.label, k.cmdlet("New"))
	}
	return obj, nil
}

// getVoiceApp reads one object by identity.
func getVoiceApp(ctx context.Context, api voiceAppsInvoker, k voiceAppKind, id string) (map[string]any, error) {
	res, err := api.Invoke(ctx, k.op("Get", http.MethodGet, k.itemPath()), map[string]any{"Identity": id})
	if err != nil {
		return nil, err
	}
	obj := k.unwrap(res.Value)
	if obj == nil {
		return nil, fmt.Errorf("no %s object in the %s response for %q", k.label, k.cmdlet("Get"), id)
	}
	return obj, nil
}

// putVoiceApp replaces an object with the given full object.
func putVoiceApp(ctx context.Context, api voiceAppsInvoker, k voiceAppKind, id string, obj map[string]any) error {
	return putVoiceAppWithQuery(ctx, api, k, id, "", obj)
}

// putVoiceAppWithQuery replaces an object, appending query to the request path.
func putVoiceAppWithQuery(ctx context.Context, api voiceAppsInvoker, k voiceAppKind, id, query string, obj map[string]any) error {
	params := make(map[string]any, len(obj)+1)
	for key, v := range obj {
		params[key] = v
	}
	params["Identity"] = id
	_, err := api.Invoke(ctx, k.op("Set", http.MethodPut, k.itemPath()+query), params)
	return err
}

// deleteVoiceApp removes an object by identity.
func deleteVoiceApp(ctx context.Context, api voiceAppsInvoker, k voiceAppKind, id string) error {
	_, err := api.Invoke(ctx, k.op("Remove", http.MethodDelete, k.itemPath()), map[string]any{"Identity": id})
	return err
}

// unwrap returns the object from a response shaped as {"<wrapper>": {...}},
// {"<listWrapper>": [{...}]} or a bare object.
func (k voiceAppKind) unwrap(v []map[string]any) map[string]any {
	obj := firstObject(v)
	if obj == nil {
		return nil
	}
	if inner, ok := obj[k.wrapper].(map[string]any); ok {
		return inner
	}
	if k.listWrapper != "" {
		if list, ok := obj[k.listWrapper].([]any); ok {
			if len(list) == 0 {
				return nil
			}
			inner, _ := list[0].(map[string]any)
			return inner
		}
	}
	return obj
}

// voiceAppID returns the id of a stored voice app object.
func voiceAppID(obj map[string]any) string {
	return firstNonEmptyStr(getString(obj, "Identity"), getString(obj, "Id"))
}

// isAPIErrorWith reports whether err is an APIError with one of the statuses whose
// message or body contains one of the phrases (case-insensitive).
func isAPIErrorWith(err error, statuses []int, phrases ...string) bool {
	var ae *teamsapi.APIError
	if !errors.As(err, &ae) {
		return false
	}
	statusOK := false
	for _, s := range statuses {
		if ae.Status == s {
			statusOK = true
			break
		}
	}
	if !statusOK {
		return false
	}
	msg, body := strings.ToLower(ae.Message), strings.ToLower(ae.Body)
	for _, p := range phrases {
		p = strings.ToLower(p)
		if strings.Contains(msg, p) || strings.Contains(body, p) {
			return true
		}
	}
	return false
}

// overlayObject returns a copy of dst with src's fields written over it. Nested
// objects are overlaid recursively; lists of objects carrying an "Id" are matched
// by Id and overlaid element-wise; any other value replaces the dst value.
func overlayObject(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst)+len(src))
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		out[k] = overlayValue(out[k], v)
	}
	return out
}

func overlayValue(dst, src any) any {
	switch s := src.(type) {
	case map[string]any:
		if d, ok := dst.(map[string]any); ok {
			return overlayObject(d, s)
		}
	case []any:
		d, ok := dst.([]any)
		if !ok {
			return s
		}
		byID := map[string]map[string]any{}
		for _, e := range d {
			if m, ok := e.(map[string]any); ok {
				if id := getString(m, "Id"); id != "" {
					byID[strings.ToLower(id)] = m
				}
			}
		}
		out := make([]any, 0, len(s))
		for _, e := range s {
			m, ok := e.(map[string]any)
			if !ok {
				out = append(out, e)
				continue
			}
			if prev, ok := byID[strings.ToLower(getString(m, "Id"))]; ok && getString(m, "Id") != "" {
				out = append(out, overlayObject(prev, m))
				continue
			}
			out = append(out, m)
		}
		return out
	}
	return src
}

// newVoiceAppID returns a random version 4 GUID string.
func newVoiceAppID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// isCallQueueGone reports whether err is a 404 or a 400 "Invalid identity".
func isCallQueueGone(err error) bool {
	if err == nil {
		return false
	}
	return isNotFound(err) || isAPIErrorWith(err, []int{http.StatusBadRequest}, "Invalid identity")
}

// isScheduleGone reports whether err means the schedule does not exist: a 404, or
// a 400/404 whose message contains "not found" or "Invalid identity".
func isScheduleGone(err error) bool {
	if err == nil {
		return false
	}
	return isNotFound(err) ||
		isAPIErrorWith(err, []int{http.StatusBadRequest, http.StatusNotFound}, "not found", "Invalid identity")
}

// isAutoAttendantGone reports whether err means the auto attendant does not exist.
func isAutoAttendantGone(err error) bool {
	return err != nil && isNotFound(err)
}

// readString copies a string field onto dst when present.
func readString(obj map[string]any, key string, dst *types.String) {
	if s, ok := obj[key].(string); ok {
		*dst = types.StringValue(s)
	}
}

// readBool copies a bool field onto dst when present.
func readBool(obj map[string]any, key string, dst *types.Bool) {
	if b, ok := obj[key].(bool); ok {
		*dst = types.BoolValue(b)
	}
}

// readInt64 copies a numeric field onto dst when present.
func readInt64(obj map[string]any, key string, dst *types.Int64) {
	switch n := obj[key].(type) {
	case float64:
		*dst = types.Int64Value(int64(n))
	case int64:
		*dst = types.Int64Value(n)
	case int:
		*dst = types.Int64Value(int64(n))
	}
}

// readEnum copies an enum field onto dst when present. field names the value in
// diagnostics; wordsOnly rejects numeric values.
func readEnum(obj map[string]any, key, field string, e wireEnum, wordsOnly bool, dst *types.String) diag.Diagnostics {
	var diags diag.Diagnostics
	v, ok := obj[key]
	if !ok || v == nil {
		return diags
	}
	var w string
	if wordsOnly {
		w, ok = e.fromWireWord(v)
	} else {
		w, ok = e.fromWire(v)
	}
	if !ok {
		diags.AddError("Unexpected "+field+" value", fmt.Sprintf("Teams returned %s = %v, which this provider does not recognise.", field, v))
		return diags
	}
	*dst = types.StringValue(w)
	return diags
}

// optStringValue maps a nullable read-back string. An empty string stays null
// unless the prior value was an empty string.
func optStringValue(v any, prior types.String) types.String {
	s, ok := v.(string)
	if !ok {
		return types.StringNull()
	}
	if s == "" && (prior.IsNull() || prior.IsUnknown()) {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// optStringWire returns the string value, or nil when null or unknown.
func optStringWire(s types.String) any {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	return s.ValueString()
}

// listStrings returns the list's strings, or nil when null or unknown.
func listStrings(l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]string, 0, len(l.Elements()))
	for _, e := range l.Elements() {
		if s, ok := e.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
			out = append(out, s.ValueString())
		}
	}
	return out
}

// stringsOf returns the strings of a JSON list, skipping other elements.
func stringsOf(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// objectsOf returns the objects of a JSON list, skipping other elements.
func objectsOf(v any) []map[string]any {
	items, _ := v.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func stringListValue(ss []string) (types.List, diag.Diagnostics) {
	vals := make([]attr.Value, 0, len(ss))
	for _, s := range ss {
		vals = append(vals, types.StringValue(s))
	}
	return types.ListValue(types.StringType, vals)
}

// optListValue maps a read-back list for an Optional attribute: empty stays null
// unless the prior value was an empty list.
func optListValue(ss []string, prior types.List) (types.List, diag.Diagnostics) {
	if len(ss) == 0 {
		if !prior.IsNull() && !prior.IsUnknown() && len(prior.Elements()) == 0 {
			return prior, nil
		}
		return types.ListNull(types.StringType), nil
	}
	return stringListValue(ss)
}

// int64Range checks that an integer is within [lo, hi].
type int64Range struct{ lo, hi int64 }

func (v int64Range) Description(_ context.Context) string {
	return fmt.Sprintf("value must be between %d and %d", v.lo, v.hi)
}

func (v int64Range) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v int64Range) ValidateInt64(ctx context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if n := req.ConfigValue.ValueInt64(); n < v.lo || n > v.hi {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid attribute value", fmt.Sprintf("%d is not valid; %s.", n, v.Description(ctx)))
	}
}

// provisioningWait bounds how long writes wait for a new resource account to reach Teams.
var provisioningWait = struct {
	timeout, interval time.Duration
}{timeout: 10 * time.Minute, interval: 10 * time.Second}

// retryWhileProvisioning calls fn until it succeeds, returns an error that
// still provisioning does not match, or the wait runs out.
func retryWhileProvisioning(ctx context.Context, fn func() error, stillProvisioning func(error) bool) error {
	deadline := time.Now().Add(provisioningWait.timeout)
	for {
		err := fn()
		if err == nil || !stillProvisioning(err) || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(provisioningWait.interval):
		}
	}
}
