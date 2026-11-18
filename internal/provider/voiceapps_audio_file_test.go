package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
)

// fakeAudioAPI answers Invoke calls by "<METHOD> <path>", with {identity} substituted.
type fakeAudioAPI struct {
	responses map[string]fakeAudioResponse
	calls     []fakeAudioCall
}

type fakeAudioResponse struct {
	value []map[string]any
	err   error
}

type fakeAudioCall struct {
	op     teamsapi.Op
	path   string
	params map[string]any
}

func (f *fakeAudioAPI) Invoke(_ context.Context, op teamsapi.Op, params map[string]any) (*teamsapi.Result, error) {
	id, _ := params["Identity"].(string)
	p := strings.ReplaceAll(op.Path, "{identity}", id)
	f.calls = append(f.calls, fakeAudioCall{op: op, path: p, params: params})
	r, ok := f.responses[op.Method+" "+p]
	if !ok {
		return nil, &teamsapi.APIError{Status: http.StatusInternalServerError, Message: "unexpected call " + op.Method + " " + p}
	}
	if r.err != nil {
		return nil, r.err
	}
	return &teamsapi.Result{Value: r.value}, nil
}

const (
	testAudioID       = "8bc2a0f1d6e94c2b9f3e7a1d5c4b2e10"
	testAudioItemPath = "/Teams.MediaStorage/audiofile/OrgAutoAttendant/" + testAudioID
	testAudioListPath = "/Teams.MediaStorage/audiofile/OrgAutoAttendant"
)

func testAudioObject(deletion any) map[string]any {
	return map[string]any{
		"Id":                      testAudioID,
		"ApplicationId":           "OrgAutoAttendant",
		"OriginalFilename":        "greeting.wav",
		"ConvertedFilename":       "greeting.wma",
		"Duration":                "00:00:03",
		"DeletionTimestampOffset": deletion,
		"DownloadUri":             "https://example.invalid/greeting",
		"UploadedTimestampOffset": "2026-11-17T10:00:00+00:00",
		"Content":                 "",
	}
}

func TestAudioFileUsages(t *testing.T) {
	cases := []struct{ tf, wire string }{
		{"auto_attendant", "OrgAutoAttendant"},
		{"call_queue", "HuntGroup"},
		{"tenant_global", "TenantGlobal"},
	}
	for _, tc := range cases {
		if got, ok := audioFileUsages.toWire(tc.tf); !ok || got != tc.wire {
			t.Errorf("toWire(%q) = %q, %v; want %q", tc.tf, got, ok, tc.wire)
		}
		if got, ok := audioFileUsages.fromWireWord(tc.wire); !ok || got != tc.tf {
			t.Errorf("fromWireWord(%q) = %q, %v; want %q", tc.wire, got, ok, tc.tf)
		}
	}
	if _, ok := audioFileUsages.toWire("bogus"); ok {
		t.Error("toWire(bogus) should fail")
	}
	if _, ok := audioFileUsages.fromWireWord(float64(0)); ok {
		t.Error("fromWireWord(0) should fail")
	}
}

func TestAudioFileUploadBody(t *testing.T) {
	content := []byte("RIFF\x00\x01\x02WAVE")
	body := audioFileUploadBody("HuntGroup", "hold.wav", content)
	if len(body) != 3 || body["ApplicationId"] != "HuntGroup" || body["OriginalFilename"] != "hold.wav" {
		t.Errorf("body = %#v", body)
	}
	if _, ok := body["FileName"]; ok {
		t.Error("body must not include FileName")
	}
	decoded, err := base64.StdEncoding.DecodeString(body["Content"].(string))
	if err != nil || !bytes.Equal(decoded, content) {
		t.Errorf("Content = %v (%v); want base64 of the file", body["Content"], err)
	}
}

func TestUploadAudioFile(t *testing.T) {
	api := &fakeAudioAPI{responses: map[string]fakeAudioResponse{
		"POST /Teams.MediaStorage/audiofile": {value: []map[string]any{testAudioObject("2026-11-19T10:00:00+00:00")}},
	}}
	obj, err := uploadAudioFile(context.Background(), api, "OrgAutoAttendant", "greeting.wav", []byte("abc"))
	if err != nil {
		t.Fatalf("uploadAudioFile: %v", err)
	}
	if getString(obj, "Id") != testAudioID {
		t.Errorf("obj = %#v", obj)
	}
	if len(api.calls) != 1 {
		t.Fatalf("calls = %d", len(api.calls))
	}
	op := api.calls[0].op
	if op.CmdletName != "Import-CsOnlineAudioFile" || op.Transport != teamsapi.ConfigAPI || op.Kind != teamsapi.AutoRest {
		t.Errorf("op = %+v", op)
	}
	if want := audioFileUploadBody("OrgAutoAttendant", "greeting.wav", []byte("abc")); !reflect.DeepEqual(api.calls[0].params, want) {
		t.Errorf("params = %#v; want %#v", api.calls[0].params, want)
	}
}

func TestCheckAudioFileName(t *testing.T) {
	for _, ok := range []string{"a.wav", "b.MP3", "c.wma", "dir/d.Wav"} {
		if err := checkAudioFileName(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"a.ogg", "b", "c.wav.txt", ".mp4"} {
		if err := checkAudioFileName(bad); err == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
}

func TestCheckAudioFileSize(t *testing.T) {
	if err := checkAudioFileSize(audioFileMaxBytes); err != nil {
		t.Errorf("5 MB: %v", err)
	}
	if err := checkAudioFileSize(audioFileMaxBytes + 1); err == nil {
		t.Error("5 MB + 1 byte should be rejected")
	}
}

func TestReadAudioSource(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "a.wav")
	if err := os.WriteFile(small, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, sum, err := readAudioSource(small)
	if err != nil {
		t.Fatalf("readAudioSource: %v", err)
	}
	if string(content) != "hello" || sum != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Errorf("content %q sum %s", content, sum)
	}

	big := filepath.Join(dir, "big.wav")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(audioFileMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAudioSource(big); err == nil {
		t.Error("a file over 5 MB should be rejected")
	}
	if _, _, err := readAudioSource(filepath.Join(dir, "missing.wav")); err == nil {
		t.Error("a missing file should be rejected")
	}
	if _, _, err := readAudioSource(dir); err == nil {
		t.Error("a directory should be rejected")
	}
}

func TestValidateAudioFile(t *testing.T) {
	cases := []struct {
		name             string
		source, fileName types.String
		wantErr          bool
	}{
		{"source extension", types.StringValue("/x/greeting.wav"), types.StringNull(), false},
		{"bad source extension", types.StringValue("/x/greeting.ogg"), types.StringNull(), true},
		{"file name overrides source", types.StringValue("/x/greeting.bin"), types.StringValue("greeting.mp3"), false},
		{"bad file name", types.StringValue("/x/greeting.wav"), types.StringValue("greeting.txt"), true},
		{"unknown source", types.StringUnknown(), types.StringNull(), false},
		{"unknown file name", types.StringValue("/x/greeting.ogg"), types.StringUnknown(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := audioFileModel{Source: tc.source, FileName: tc.fileName}
			if got := validateAudioFile(&m).HasError(); got != tc.wantErr {
				t.Errorf("HasError = %v; want %v", got, tc.wantErr)
			}
		})
	}
}

func TestAudioFileHashPlan(t *testing.T) {
	cases := []struct {
		name        string
		prior       types.String
		wantReplace bool
	}{
		{"create", types.StringNull(), false},
		{"unknown prior", types.StringUnknown(), false},
		{"same hash", types.StringValue("abc"), false},
		{"changed hash", types.StringValue("old"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			planned, replace := audioFileHashPlan(tc.prior, "abc")
			if !planned.Equal(types.StringValue("abc")) || replace != tc.wantReplace {
				t.Errorf("audioFileHashPlan = %s, %v; want abc, %v", planned, replace, tc.wantReplace)
			}
		})
	}
}

func TestGetAudioFile(t *testing.T) {
	badRequest := &teamsapi.APIError{Status: http.StatusBadRequest, Message: "bad request"}
	cases := []struct {
		name      string
		responses map[string]fakeAudioResponse
		wantGone  bool
		wantErr   bool
	}{
		{"found", map[string]fakeAudioResponse{"GET " + testAudioItemPath: {value: []map[string]any{testAudioObject(nil)}}}, false, false},
		{"404", map[string]fakeAudioResponse{"GET " + testAudioItemPath: {err: &teamsapi.APIError{Status: http.StatusNotFound, Message: "Not Found"}}}, true, false},
		{"400 and missing from list", map[string]fakeAudioResponse{
			"GET " + testAudioItemPath: {err: badRequest},
			"GET " + testAudioListPath: {value: []map[string]any{{"Id": "other"}}},
		}, true, false},
		{"400 and in list", map[string]fakeAudioResponse{
			"GET " + testAudioItemPath: {err: badRequest},
			"GET " + testAudioListPath: {value: []map[string]any{{"Id": strings.ToUpper(testAudioID)}}},
		}, false, true},
		{"400 and list fails", map[string]fakeAudioResponse{"GET " + testAudioItemPath: {err: badRequest}}, false, true},
		{"500", map[string]fakeAudioResponse{"GET " + testAudioItemPath: {err: &teamsapi.APIError{Status: 500, Message: "boom"}}}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAudioAPI{responses: tc.responses}
			obj, gone, err := getAudioFile(context.Background(), api, "OrgAutoAttendant", testAudioID)
			if gone != tc.wantGone || (err != nil) != tc.wantErr {
				t.Errorf("gone = %v, err = %v; want gone %v, error %v", gone, err, tc.wantGone, tc.wantErr)
			}
			if !tc.wantGone && !tc.wantErr && getString(obj, "Id") != testAudioID {
				t.Errorf("obj = %#v", obj)
			}
		})
	}
}

func TestDeleteAudioFile(t *testing.T) {
	scheduledErr := &teamsapi.APIError{Status: http.StatusBadRequest, Message: "Exception encountered while validating the audio file before upload."}
	cases := []struct {
		name        string
		responses   map[string]fakeAudioResponse
		wantWarning bool
		wantErr     bool
	}{
		{"deleted", map[string]fakeAudioResponse{"DELETE " + testAudioItemPath: {}}, false, false},
		{"scheduled for deletion", map[string]fakeAudioResponse{
			"DELETE " + testAudioItemPath: {err: scheduledErr},
			"GET " + testAudioItemPath:    {value: []map[string]any{testAudioObject("2026-11-19T10:00:00+00:00")}},
		}, true, false},
		{"already gone", map[string]fakeAudioResponse{
			"DELETE " + testAudioItemPath: {err: &teamsapi.APIError{Status: http.StatusNotFound, Message: "Not Found"}},
			"GET " + testAudioItemPath:    {err: &teamsapi.APIError{Status: http.StatusNotFound, Message: "Not Found"}},
		}, true, false},
		{"linked file", map[string]fakeAudioResponse{
			"DELETE " + testAudioItemPath: {err: scheduledErr},
			"GET " + testAudioItemPath:    {value: []map[string]any{testAudioObject(nil)}},
		}, false, true},
		{"read fails", map[string]fakeAudioResponse{
			"DELETE " + testAudioItemPath: {err: scheduledErr},
			"GET " + testAudioItemPath:    {err: &teamsapi.APIError{Status: 500, Message: "boom"}},
		}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAudioAPI{responses: tc.responses}
			diags := deleteAudioFile(context.Background(), api, "OrgAutoAttendant", testAudioID)
			if diags.HasError() != tc.wantErr {
				t.Errorf("HasError = %v; want %v (%v)", diags.HasError(), tc.wantErr, diags)
			}
			warnings := diags.Warnings()
			if (len(warnings) == 1) != tc.wantWarning {
				t.Errorf("warnings = %v; want warning %v", warnings, tc.wantWarning)
			}
			if tc.wantWarning && warnings[0].Detail() != audioFileUnlinkedWarning {
				t.Errorf("warning detail = %q", warnings[0].Detail())
			}
			if tc.wantErr && diags.Errors()[0].Summary() != "Remove-CsOnlineAudioFile failed" {
				t.Errorf("error summary = %q", diags.Errors()[0].Summary())
			}
		})
	}
}

func TestAudioFileScheduled(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want bool
	}{{nil, false}, {"", false}, {"2026-11-19T10:00:00+00:00", true}} {
		if got := audioFileScheduled(map[string]any{"DeletionTimestampOffset": tc.v}); got != tc.want {
			t.Errorf("audioFileScheduled(%#v) = %v; want %v", tc.v, got, tc.want)
		}
	}
	if audioFileScheduled(map[string]any{}) {
		t.Error("a missing DeletionTimestampOffset is not scheduled")
	}
}

func TestParseAudioFileImportID(t *testing.T) {
	usage, id, err := parseAudioFileImportID("auto_attendant/" + testAudioID)
	if err != nil || usage != "auto_attendant" || id != testAudioID {
		t.Errorf("parseAudioFileImportID = %q, %q, %v", usage, id, err)
	}
	if usage, _, err := parseAudioFileImportID("call_queue/x"); err != nil || usage != "call_queue" {
		t.Errorf("call_queue: %q, %v", usage, err)
	}
	for _, bad := range []string{testAudioID, "auto_attendant/", "/" + testAudioID, "bogus/" + testAudioID, "OrgAutoAttendant/" + testAudioID, "auto_attendant/a/b"} {
		if _, _, err := parseAudioFileImportID(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestAudioFileModelFromObject(t *testing.T) {
	m := audioFileModel{
		ID: types.StringUnknown(), Usage: types.StringValue("auto_attendant"), Source: types.StringValue("/x/greeting.wav"),
		FileName: types.StringValue("greeting.wav"), SourceSHA256: types.StringValue("abc"),
		Duration: types.StringUnknown(), ScheduledForDeletion: types.BoolUnknown(),
	}
	if diags := audioFileModelFromObject(testAudioObject("2026-11-19T10:00:00+00:00"), &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	want := audioFileModel{
		ID: types.StringValue(testAudioID), Usage: types.StringValue("auto_attendant"), Source: types.StringValue("/x/greeting.wav"),
		FileName: types.StringValue("greeting.wav"), SourceSHA256: types.StringValue("abc"),
		Duration: types.StringValue("00:00:03"), ScheduledForDeletion: types.BoolValue(true),
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("model = %+v\nwant %+v", m, want)
	}

	// Import: only id and usage are known.
	imported := audioFileModel{
		ID: types.StringValue(testAudioID), Usage: types.StringValue("auto_attendant"), Source: types.StringNull(),
		FileName: types.StringNull(), SourceSHA256: types.StringNull(), Duration: types.StringNull(), ScheduledForDeletion: types.BoolNull(),
	}
	if diags := audioFileModelFromObject(testAudioObject(nil), &imported); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if !imported.FileName.Equal(types.StringValue("greeting.wav")) || !imported.ScheduledForDeletion.Equal(types.BoolValue(false)) ||
		!imported.Source.IsNull() || !imported.SourceSHA256.IsNull() {
		t.Errorf("imported = %+v", imported)
	}

	bad := testAudioObject(nil)
	bad["ApplicationId"] = "Bogus"
	d := audioFileModelFromObject(bad, &imported)
	if !d.HasError() || d.Errors()[0].Summary() != "Unexpected ApplicationId value" {
		t.Errorf("diagnostics = %v", d)
	}
}
