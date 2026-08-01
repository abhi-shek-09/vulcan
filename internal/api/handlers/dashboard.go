package handlers

import (
	"net/http"
	"github.com/go-chi/chi/v5"
	"vulcan/internal/api/response"
	"vulcan/internal/dashboard"
)


type DashboardHandler struct {
	service *dashboard.Service
}


func NewDashboardHandler(
	service *dashboard.Service,
) *DashboardHandler {
	return &DashboardHandler{
		service: service,
	}
}

func (h *DashboardHandler) GetMetrics(w http.ResponseWriter, r *http.Request){
	testID := chi.URLParam(r,"id")
	metrics,err := h.service.GetLiveMetrics(testID)
	if err != nil {
		response.JSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error":err.Error(),
			},
		)
		return
	}
	response.JSON(
		w,
		http.StatusOK,
		metrics,
	)
}

func (h *DashboardHandler) GetHistory(
	w http.ResponseWriter,
	r *http.Request,
){

	testID:=chi.URLParam(r,"id")

	start:=r.URL.Query().Get("start")
	end:=r.URL.Query().Get("end")
	step:=r.URL.Query().Get("step")

	result,err:=h.service.GetHistory(
		testID,
		start,
		end,
		step,
	)
	if err!=nil{
		response.JSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error":err.Error(),
			},
		)
		return
	}
	response.JSON(
		w,
		http.StatusOK,
		result,
	)
}