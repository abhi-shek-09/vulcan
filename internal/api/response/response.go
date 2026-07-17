// package response

// import (
// 	"encoding/json"
// 	"net/http"
// )

// func JSON(w http.ResponseWriter, status int, data any) {
// 	w.Header().Set("Content-Type", "application/json")
// 	w.WriteHeader(status)

// 	if err := json.NewEncoder(w).Encode(data); err != nil {
// 		http.Error(w, "failed to encode response", http.StatusInternalServerError)
// 	}
// }

package response

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, data any) {
	var buf bytes.Buffer

	if err := json.NewEncoder(&buf).Encode(data); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_, _ = w.Write(buf.Bytes())
}
