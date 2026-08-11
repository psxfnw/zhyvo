package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photodrop/internal/problemreport"
)

type problemReportHandler struct {
	service *problemreport.Service
}

type createProblemReportRequest struct {
	Category         string                         `json:"category"`
	Description      string                         `json:"description"`
	Contact          string                         `json:"contact"`
	TechnicalContext problemreport.TechnicalContext `json:"technical_context"`
}

type updateProblemReportRequest struct {
	Status    string `json:"status"`
	AdminNote string `json:"admin_note"`
}

func (handler problemReportHandler) create(response http.ResponseWriter, request *http.Request) {
	var input createProblemReportRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	var reporterID *uuid.UUID
	if principal, ok := principalFromContext(request.Context()); ok {
		reporterID = &principal.IdentityID
	}
	result, err := handler.service.Create(request.Context(), reporterID, problemreport.CreateInput{
		Category: input.Category, Description: input.Description, Contact: input.Contact, TechnicalContext: input.TechnicalContext,
	})
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"report": result})
}

func (handler problemReportHandler) list(response http.ResponseWriter, request *http.Request) {
	result, err := handler.service.List(request.Context(), strings.TrimSpace(request.URL.Query().Get("status")), strings.TrimSpace(request.URL.Query().Get("category")))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"reports": result})
}

func (handler problemReportHandler) get(response http.ResponseWriter, request *http.Request) {
	id, err := uuid.Parse(chi.URLParam(request, "reportID"))
	if err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_REPORT_ID", "Report ID is invalid")
		return
	}
	result, err := handler.service.Get(request.Context(), id)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"report": result})
}

func (handler problemReportHandler) update(response http.ResponseWriter, request *http.Request) {
	id, err := uuid.Parse(chi.URLParam(request, "reportID"))
	if err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_REPORT_ID", "Report ID is invalid")
		return
	}
	var input updateProblemReportRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	result, err := handler.service.Update(request.Context(), id, input.Status, input.AdminNote)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"report": result})
}

func (handler problemReportHandler) stats(response http.ResponseWriter, request *http.Request) {
	result, err := handler.service.Stats(request.Context())
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler problemReportHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, problemreport.ErrInvalidInput):
		writeAPIError(response, request, http.StatusUnprocessableEntity, "INVALID_PROBLEM_REPORT", "Check the category and enter 10 to 2000 characters")
	case errors.Is(err, problemreport.ErrNotFound):
		writeAPIError(response, request, http.StatusNotFound, "PROBLEM_REPORT_NOT_FOUND", "Problem report not found")
	default:
		writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}
