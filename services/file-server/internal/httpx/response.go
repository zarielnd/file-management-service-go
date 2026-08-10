package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/apperror"
)

func WriteJSON(w http.ResponseWriter, status int, payload any){
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Encode the payload as JSON and write it to the response
	// Handle any encoding errors if necessary
	if payload != nil {
		_ = json.NewEncoder(w).Encode(payload)
	}
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteError(w http.ResponseWriter, err error){
	appError, ok := err.(*apperror.Error)
	if !ok {
		appError = apperror.Internal(err.Error())
	}

	WriteJSON(w, appError.HTTPStatus, errorResponse{
		Error: errorBody{
			Code:    string(appError.Code),
			Message: appError.Message,
		},
	})
}