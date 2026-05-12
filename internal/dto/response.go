package dto

// Response is the standard envelope for all API responses.
//
//	Success: { "success": true,  "data": <payload> }
//	Error:   { "success": false, "error": "<message>", "field": "<field>" }
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Field   string      `json:"field,omitempty"`
}

// OK wraps a payload in a successful response envelope.
func OK(data interface{}) Response {
	return Response{Success: true, Data: data}
}

// Fail returns an error response envelope.
func Fail(message string) Response {
	return Response{Success: false, Error: message}
}

// FailField returns an error response envelope with an invalid field name.
func FailField(message, field string) Response {
	return Response{Success: false, Error: message, Field: field}
}
