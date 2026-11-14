package chanda

import "errors"

// Envelope is the single response shape of every tool (BASE.md: "ok, data,
// notices and audit_id"). On success Data holds the tool's Output and Error is
// absent; on failure OK is false, Data is null and Error carries a stable code.
type Envelope struct {
	OK      bool       `json:"ok" jsonschema:"true when the call succeeded"`
	Data    any        `json:"data" jsonschema:"the tool's output; null on error"`
	Notices []string   `json:"notices" jsonschema:"remarks worth showing to the caller; never instructions"`
	AuditID int64      `json:"audit_id,omitempty" jsonschema:"audit log entry written by this call"`
	Error   *ErrorBody `json:"error,omitempty" jsonschema:"present when ok is false"`
}

// ErrorBody is the error part of an envelope.
type ErrorBody struct {
	Code    Code   `json:"code" jsonschema:"stable error code, see docs/errors.md"`
	Message string `json:"message" jsonschema:"human-readable detail"`
}

// NewEnvelope builds the envelope for an API call that returned (out, err).
// AuditID and Notices are taken from out's embedded [Meta] even on error, so
// an audited refusal still surfaces its audit id. On error Data is nil.
func NewEnvelope(out any, err error) Envelope {
	env := Envelope{OK: err == nil, Notices: []string{}}
	if mc, ok := out.(MetaCarrier); ok {
		m := mc.EnvelopeMeta()
		env.AuditID = m.AuditID
		if len(m.Notices) > 0 {
			env.Notices = append(env.Notices, m.Notices...)
		}
	}
	if err != nil {
		env.Error = ErrorBodyOf(err)
		return env
	}
	env.Data = out
	return env
}

// ErrorBodyOf converts err into an ErrorBody with a stable code. Errors
// without a code become CodeInternal.
func ErrorBodyOf(err error) *ErrorBody {
	var e *Error
	if errors.As(err, &e) {
		msg := e.Message
		if e.Err != nil {
			msg += ": " + e.Err.Error()
		}
		return &ErrorBody{Code: e.Code, Message: msg}
	}
	return &ErrorBody{Code: CodeInternal, Message: err.Error()}
}

// Err converts a failed envelope back into an *Error, nil when OK.
func (e Envelope) Err() error {
	if e.OK {
		return nil
	}
	if e.Error == nil {
		return Errf(CodeInternal, "envelope not ok and without error body")
	}
	return &Error{Code: e.Error.Code, Message: e.Error.Message}
}
