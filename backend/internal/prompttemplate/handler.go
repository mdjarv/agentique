package prompttemplate

import (
	"encoding/json"
	"net/http"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// Handler handles HTTP requests for prompt template CRUD operations.
type Handler struct {
	Queries *store.Queries
}

type createRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Settings    string `json:"settings"`
	Tags        string `json:"tags"`
}

type updateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Content     *string `json:"content"`
	Settings    *string `json:"settings"`
	Tags        *string `json:"tags"`
}

// HandleList returns all prompt templates.
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	templates, err := h.Queries.ListPromptTemplates(r.Context())
	if err != nil {
		httperror.RespondError(w, httperror.Internal("list templates", err))
		return
	}
	httperror.JSON(w, http.StatusOK, templates)
}

// HandleGet returns a single prompt template by ID.
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httperror.RespondError(w, httperror.BadRequest("id is required"))
		return
	}
	tmpl, err := h.Queries.GetPromptTemplate(r.Context(), id)
	if err != nil {
		httperror.RespondError(w, err)
		return
	}
	httperror.JSON(w, http.StatusOK, tmpl)
}

// HandleCreate creates a new prompt template.
func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid JSON body"))
		return
	}
	if req.Name == "" {
		httperror.RespondError(w, httperror.BadRequest("name is required"))
		return
	}

	settings := req.Settings
	if settings == "" {
		settings = "{}"
	}
	tags := req.Tags
	if tags == "" {
		tags = "[]"
	}

	tmpl, err := h.Queries.CreatePromptTemplate(r.Context(), store.CreatePromptTemplateParams{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Settings:    settings,
		Tags:        tags,
	})
	if err != nil {
		httperror.RespondError(w, err)
		return
	}
	httperror.JSON(w, http.StatusCreated, tmpl)
}

// HandleUpdate updates an existing prompt template.
func (h *Handler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httperror.RespondError(w, httperror.BadRequest("id is required"))
		return
	}

	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid JSON body"))
		return
	}

	existing, err := h.Queries.GetPromptTemplate(r.Context(), id)
	if err != nil {
		httperror.RespondError(w, err)
		return
	}

	name := existing.Name
	if req.Name != nil {
		name = *req.Name
	}
	description := existing.Description
	if req.Description != nil {
		description = *req.Description
	}
	content := existing.Content
	if req.Content != nil {
		content = *req.Content
	}
	settings := existing.Settings
	if req.Settings != nil {
		settings = *req.Settings
	}
	tags := existing.Tags
	if req.Tags != nil {
		tags = *req.Tags
	}

	tmpl, err := h.Queries.UpdatePromptTemplate(r.Context(), store.UpdatePromptTemplateParams{
		ID:          id,
		Name:        name,
		Description: description,
		Content:     content,
		Settings:    settings,
		Tags:        tags,
	})
	if err != nil {
		httperror.RespondError(w, err)
		return
	}
	httperror.JSON(w, http.StatusOK, tmpl)
}

// HandleDelete deletes a prompt template by ID.
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httperror.RespondError(w, httperror.BadRequest("id is required"))
		return
	}
	if err := h.Queries.DeletePromptTemplate(r.Context(), id); err != nil {
		httperror.RespondError(w, httperror.Internal("delete template", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
