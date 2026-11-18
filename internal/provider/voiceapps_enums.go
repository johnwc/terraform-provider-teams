package provider

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// wireEnum maps terraform enum words to Teams enum words. The position of each
// pair is the integer value Teams returns on read.
type wireEnum struct {
	tfWords   []string
	wireWords []string
}

func newWireEnum(pairs ...[2]string) wireEnum {
	e := wireEnum{}
	for _, p := range pairs {
		e.tfWords = append(e.tfWords, p[0])
		e.wireWords = append(e.wireWords, p[1])
	}
	return e
}

// toWire returns the Teams word for a terraform word.
func (e wireEnum) toWire(tfWord string) (string, bool) {
	for i, w := range e.tfWords {
		if w == tfWord {
			return e.wireWords[i], true
		}
	}
	return "", false
}

// fromWire returns the terraform word for a read-back value, which is either a
// JSON number (the enum ordinal) or a Teams word.
func (e wireEnum) fromWire(v any) (string, bool) {
	idx := -1
	switch n := v.(type) {
	case float64:
		if n == math.Trunc(n) {
			idx = int(n)
		}
	case int:
		idx = n
	case int64:
		idx = int(n)
	case string:
		for i, w := range e.wireWords {
			if strings.EqualFold(w, n) {
				return e.tfWords[i], true
			}
		}
		return "", false
	}
	if idx < 0 || idx >= len(e.tfWords) {
		return "", false
	}
	return e.tfWords[idx], true
}

// stringValidator returns a validator accepting only the terraform words.
func (e wireEnum) stringValidator() validator.String { return enumValidator{words: e.tfWords} }

var (
	routingMethods = newWireEnum(
		[2]string{"attendant", "Attendant"},
		[2]string{"serial", "Serial"},
		[2]string{"round_robin", "RoundRobin"},
		[2]string{"longest_idle", "LongestIdle"},
	)
	overflowActions = newWireEnum(
		[2]string{"disconnect_with_busy", "DisconnectWithBusy"},
		[2]string{"forward", "Forward"},
		[2]string{"voicemail", "Voicemail"},
		[2]string{"shared_voicemail", "SharedVoicemail"},
	)
	timeoutActions = newWireEnum(
		[2]string{"disconnect", "Disconnect"},
		[2]string{"forward", "Forward"},
		[2]string{"voicemail", "Voicemail"},
		[2]string{"shared_voicemail", "SharedVoicemail"},
	)
	noAgentActions = newWireEnum(
		[2]string{"queue", "Queue"},
		[2]string{"disconnect", "Disconnect"},
		[2]string{"forward", "Forward"},
		[2]string{"voicemail", "Voicemail"},
		[2]string{"shared_voicemail", "SharedVoicemail"},
	)
)

// Auto attendant enums.
var (
	aaVoices = newWireEnum(
		[2]string{"female", "Female"},
		[2]string{"male", "Male"},
	)
	aaMenuKeys = newWireEnum(
		[2]string{"automatic", "Automatic"},
		[2]string{"0", "Tone0"},
		[2]string{"1", "Tone1"},
		[2]string{"2", "Tone2"},
		[2]string{"3", "Tone3"},
		[2]string{"4", "Tone4"},
		[2]string{"5", "Tone5"},
		[2]string{"6", "Tone6"},
		[2]string{"7", "Tone7"},
		[2]string{"8", "Tone8"},
		[2]string{"9", "Tone9"},
		[2]string{"star", "ToneStar"},
		[2]string{"pound", "TonePound"},
	)
	aaMenuActions = newWireEnum(
		[2]string{"disconnect", "DisconnectCall"},
		[2]string{"transfer", "TransferCallToTarget"},
		[2]string{"transfer_to_operator", "TransferCallToOperator"},
		[2]string{"announcement", "Announcement"},
	)
	aaTargetTypes = newWireEnum(
		[2]string{"user", "User"},
		[2]string{"application_endpoint", "ApplicationEndpoint"},
		[2]string{"external_pstn", "ExternalPstn"},
		[2]string{"shared_voicemail", "SharedVoicemail"},
		[2]string{"voice_app", "ConfigurationEndpoint"},
	)
)

// Resource account, association and phone number enums.
var (
	// resourceAccountTypes maps the resource account type to its Teams application id.
	resourceAccountTypes = newWireEnum(
		[2]string{"auto_attendant", "ce933385-9390-45d1-9512-c8d228074e07"},
		[2]string{"call_queue", "11cd3e2e-fccb-42ad-ad00-878b93575e07"},
	)
	associationConfigTypes = newWireEnum(
		[2]string{"auto_attendant", "AutoAttendant"},
		[2]string{"call_queue", "CallQueue"},
	)
	phoneNumberTypes = newWireEnum(
		[2]string{"calling_plan", "CallingPlan"},
		[2]string{"direct_routing", "DirectRouting"},
		[2]string{"operator_connect", "OperatorConnect"},
	)
	// audioFileUsages maps the audio file usage to its Teams application id.
	audioFileUsages = newWireEnum(
		[2]string{"auto_attendant", "OrgAutoAttendant"},
		[2]string{"call_queue", "HuntGroup"},
		[2]string{"tenant_global", "TenantGlobal"},
	)
)

// fromWireWord is fromWire restricted to Teams words; numbers are rejected.
func (e wireEnum) fromWireWord(v any) (string, bool) {
	if _, ok := v.(string); !ok {
		return "", false
	}
	return e.fromWire(v)
}

// enumValidator checks that a string is one of a fixed set of words.
type enumValidator struct{ words []string }

func (v enumValidator) Description(_ context.Context) string {
	return "value must be one of: " + strings.Join(v.words, ", ")
}

func (v enumValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v enumValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	for _, w := range v.words {
		if w == s {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid attribute value",
		fmt.Sprintf("%q is not valid; %s.", s, v.Description(ctx)))
}
