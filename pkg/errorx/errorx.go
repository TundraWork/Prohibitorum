package errorx

import "github.com/danielgtaylor/huma/v2"

type Error struct {
	status  int
	Message string   `json:"message"`
	Code    string   `json:"code"`
	Details []string `json:"details"`
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) GetStatus() int {
	return e.status
}

type codedError struct {
	code string
}

func (e *codedError) Error() string {
	return e.code
}

func ErrorCode(code string) error {
	return &codedError{code: code}
}

func init() {
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		e := &Error{
			status:  status,
			Message: msg,
			Code:    "UNKNOWN",
			Details: []string{},
		}
		for _, err := range errs {
			if codedError, ok := err.(*codedError); ok {
				e.Code = codedError.code
			} else {
				e.Details = append(e.Details, err.Error())
			}
		}
		return e
	}
}
