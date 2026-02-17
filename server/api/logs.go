package api

import (
	"net/http"
	"os"
	"path/filepath"
)

var allowedLogs = map[string]string{
	"archiveloop":  "/mutable/archiveloop.log",
	"setup":        "/sentryusb/sentryusb-setup.log",
	"diagnostics":  "/tmp/diagnostics.txt",
}

func (h *handlers) getLog(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	logPath, ok := allowedLogs[name]
	if !ok {
		writeError(w, http.StatusNotFound, "Unknown log: "+name)
		return
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "Log file not found")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(logPath)+"\"")
	w.Write(data)
}
