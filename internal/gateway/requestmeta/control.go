package requestmeta

import (
	"context"
	"net/http"
)

type controlKey struct{}

func MarkControl(request *http.Request) {
	*request = *request.WithContext(context.WithValue(request.Context(), controlKey{}, true))
}

func IsControl(request *http.Request) bool {
	return request.Context().Value(controlKey{}) == true
}
