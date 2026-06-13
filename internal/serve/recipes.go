package serve

import (
	"encoding/json"
	"net/http"

	"github.com/agentjoey/pactify/internal/recipe"
)

func (s *Server) registerRecipeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/recipes", s.handleRecipeList)
	mux.HandleFunc("POST /api/recipes/{name}/expand", s.handleRecipeExpand)
}

type recipeItemDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleRecipeList(w http.ResponseWriter, _ *http.Request) {
	out := make([]recipeItemDTO, 0)
	for _, name := range recipe.Names() {
		if r, ok := recipe.Get(name); ok {
			out = append(out, recipeItemDTO{Name: r.Name, Description: r.Description})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type recipeExpandReq struct {
	Goal string `json:"goal"`
}

type expandedTaskDTO struct {
	ID   string   `json:"id"`
	Spec string   `json:"spec"`
	Deps []string `json:"deps,omitempty"`
}

func (s *Server) handleRecipeExpand(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	rec, ok := recipe.Get(name)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown recipe")
		return
	}
	var req recipeExpandReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	tasks, err := rec.Expand(req.Goal)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]expandedTaskDTO, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, expandedTaskDTO{ID: t.ID, Spec: t.Spec, Deps: t.Deps})
	}
	writeJSON(w, http.StatusOK, out)
}
