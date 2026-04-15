package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

var supportServerURL = APIBaseURL

func supportProxy(method, path string, payload []byte, authToken string, timeout time.Duration) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, supportServerURL+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("X-Auth-Token", authToken)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBody, resp.StatusCode, nil
}

// supportProxyWithHeaders is like supportProxy but sets additional headers on the outbound request.
func supportProxyWithHeaders(method, path string, payload []byte, extraHeaders map[string]string, timeout time.Duration) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, supportServerURL+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBody, resp.StatusCode, nil
}

// sanitizeJSON re-parses and re-serializes JSON to normalize any encoding issues
// (e.g. diagnostics text containing backslash sequences like \usb that look like
// incomplete Unicode escapes to strict JSON parsers).
func sanitizeJSON(raw []byte) []byte {
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		// If it won't parse, return as-is
		return raw
	}
	clean, err := json.Marshal(parsed)
	if err != nil {
		return raw
	}
	return clean
}

// POST /api/support/ticket — create a new support ticket
func (h *handlers) createSupportTicket(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	// Re-serialize to normalize Unicode escapes in diagnostics text
	body = sanitizeJSON(body)
	log.Printf("[CHAT] Forwarding ticket creation (%d bytes)", len(body))

	respBody, status, err := supportProxy("POST", "/chat/ticket", body, "", 30*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Support server unreachable: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// POST /api/support/ticket/{id}/message — send a message
func (h *handlers) sendSupportMessage(w http.ResponseWriter, r *http.Request) {
	ticketId := r.PathValue("id")
	authToken := r.Header.Get("X-Auth-Token")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	body = sanitizeJSON(body)

	respBody, status, err := supportProxy("POST", fmt.Sprintf("/chat/ticket/%s/message", ticketId), body, authToken, 30*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Support server unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// POST /api/support/ticket/{id}/media — upload media
func (h *handlers) uploadSupportMedia(w http.ResponseWriter, r *http.Request) {
	ticketId := r.PathValue("id")
	authToken := r.Header.Get("X-Auth-Token")

	// Limit to 100MB but stream — don't buffer in RAM
	r.Body = http.MaxBytesReader(w, r.Body, 100*1024*1024)

	// Forward the body directly as a stream to the support server
	req, err := http.NewRequest("POST", supportServerURL+fmt.Sprintf("/chat/ticket/%s/media", ticketId), r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	// Preserve the original content type (multipart/form-data, etc.)
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	if authToken != "" {
		req.Header.Set("X-Auth-Token", authToken)
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Support server unreachable")
		return
	}
	defer resp.Body.Close()

	// Stream response back (small JSON)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// GET /api/support/ticket/{id}/messages — fetch messages
func (h *handlers) fetchSupportMessages(w http.ResponseWriter, r *http.Request) {
	ticketId := r.PathValue("id")
	authToken := r.Header.Get("X-Auth-Token")

	query := ""
	if since := r.URL.Query().Get("since"); since != "" {
		query = "?since=" + since
	}

	respBody, status, err := supportProxy("GET", fmt.Sprintf("/chat/ticket/%s/messages%s", ticketId, query), nil, authToken, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Support server unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// POST /api/support/ticket/{id}/close — close a ticket
func (h *handlers) closeSupportTicket(w http.ResponseWriter, r *http.Request) {
	ticketId := r.PathValue("id")
	authToken := r.Header.Get("X-Auth-Token")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	respBody, status, err := supportProxy("POST", fmt.Sprintf("/chat/ticket/%s/close", ticketId), body, authToken, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Support server unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// POST /api/support/ticket/{id}/mark-read — mark messages as read
func (h *handlers) markSupportRead(w http.ResponseWriter, r *http.Request) {
	ticketId := r.PathValue("id")
	authToken := r.Header.Get("X-Auth-Token")

	respBody, status, err := supportProxy("POST", fmt.Sprintf("/chat/ticket/%s/mark-read", ticketId), nil, authToken, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Support server unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// POST /api/support/ticket/{id}/register-device — register iOS device for push notifications
func (h *handlers) registerSupportDevice(w http.ResponseWriter, r *http.Request) {
	ticketId := r.PathValue("id")
	authToken := r.Header.Get("X-Auth-Token")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	respBody, status, err := supportProxy("POST", fmt.Sprintf("/chat/ticket/%s/register-device", ticketId), body, authToken, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Support server unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// POST /api/support/ticket/{id}/unregister-device — unregister iOS device from push notifications
func (h *handlers) unregisterSupportDevice(w http.ResponseWriter, r *http.Request) {
	ticketId := r.PathValue("id")
	authToken := r.Header.Get("X-Auth-Token")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	respBody, status, err := supportProxy("POST", fmt.Sprintf("/chat/ticket/%s/unregister-device", ticketId), body, authToken, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Support server unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// GET /api/support/check — check if support server is reachable (and internet is available)
func (h *handlers) checkSupportAvailable(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(supportServerURL + "/health")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"error":     "Cannot reach support server. Check internet connection.",
		})
		return
	}
	defer resp.Body.Close()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"available": resp.StatusCode == 200,
	})
}
