// Package accounts: customers CRUD.
package accounts

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

type Customer struct {
	ID      int64  `json:"id"`
	Ad      string `json:"ad"`
	Eposta  string `json:"eposta"`
	PlanID  *int64 `json:"plan_id"`
	Durum   string `json:"durum"`
	Notlar  string `json:"notlar"`
	Created string `json:"olusturma"`
}

type Handlers struct {
	DB *sql.DB
}

// ------------ Customers ------------

func (h *Handlers) ListCustomers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, ad, eposta, plan_id, durum, notlar, DATE_FORMAT(created_at,'%Y-%m-%d')
		 FROM customers ORDER BY id`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := make([]Customer, 0)
	for rows.Next() {
		var cs Customer
		if err := rows.Scan(&cs.ID, &cs.Ad, &cs.Eposta, &cs.PlanID, &cs.Durum, &cs.Notlar, &cs.Created); err == nil {
			out = append(out, cs)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handlers) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	var cs Customer
	if err := json.NewDecoder(r.Body).Decode(&cs); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if cs.Ad == "" || cs.Eposta == "" {
		httpx.WriteError(w, http.StatusBadRequest, "ad ve eposta zorunlu")
		return
	}
	if cs.Durum == "" {
		cs.Durum = "aktif"
	}
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO customers(ad, eposta, plan_id, durum, notlar) VALUES(?,?,?,?,?)`,
		cs.Ad, cs.Eposta, cs.PlanID, cs.Durum, cs.Notlar)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB: "+err.Error())
		return
	}
	cs.ID, _ = res.LastInsertId()
	httpx.WriteJSON(w, http.StatusCreated, cs)
}

func (h *Handlers) UpdateCustomer(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var cs Customer
	if err := json.NewDecoder(r.Body).Decode(&cs); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE customers SET ad=?, eposta=?, plan_id=?, durum=?, notlar=? WHERE id=?`,
		cs.Ad, cs.Eposta, cs.PlanID, cs.Durum, cs.Notlar, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) DeleteCustomer(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var n int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM domains WHERE customer_id=?`, id).Scan(&n); err == nil && n > 0 {
		httpx.WriteError(w, http.StatusConflict, "önce bu müşterinin domainlerini kaldırın")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM customers WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
