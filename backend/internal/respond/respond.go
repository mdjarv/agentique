package respond

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/mdjarv/agentique/backend/internal/httperr"
)

// JSON writes data as a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Error classifies err into an HTTP status code and writes a JSON error response.
// 5xx errors are logged at Error level; 4xx at Warn.
func Error(w http.ResponseWriter, err error) {
	he := httperr.Classify(err)

	if he.Status >= 500 {
		slog.Error("http error", "status", he.Status, "error", he.Message, "cause", he.Cause)
	} else {
		slog.Warn("http error", "status", he.Status, "error", he.Message)
	}

	JSON(w, he.Status, map[string]string{"error": he.Message})
}
