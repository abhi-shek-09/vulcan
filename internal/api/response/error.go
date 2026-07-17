package response

import "net/http"

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Error(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
) {
	JSON(
		w,
		status,
		ErrorResponse{
			Error: APIError{
				Code:    code,
				Message: message,
			},
		},
	)
}
