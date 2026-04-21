package planner

import "errors"

var ErrMissingAPIKey = errors.New("OPENAI_API_KEY is required for live planner calls")
var ErrMissingModel = errors.New("planner model is required for live planner calls")

type ValidationError struct {
	err         error
	rawResponse string
	rawOutput   string
	responseID  string
}

func (e ValidationError) Error() string {
	if e.err == nil {
		return "planner output failed validation"
	}
	return e.err.Error()
}

func (e ValidationError) Unwrap() error {
	return e.err
}

func NewValidationError(err error, rawResponse string, rawOutput string, responseID string) error {
	if err == nil {
		return nil
	}
	return ValidationError{
		err:         err,
		rawResponse: rawResponse,
		rawOutput:   rawOutput,
		responseID:  responseID,
	}
}

func IsValidationError(err error) bool {
	var validationError ValidationError
	return errors.As(err, &validationError)
}

func ValidationErrorData(err error) (rawResponse string, rawOutput string, responseID string, ok bool) {
	var validationError ValidationError
	if !errors.As(err, &validationError) {
		return "", "", "", false
	}

	return validationError.rawResponse, validationError.rawOutput, validationError.responseID, true
}

func IsMissingRequiredConfig(err error) bool {
	return errors.Is(err, ErrMissingAPIKey) || errors.Is(err, ErrMissingModel)
}
