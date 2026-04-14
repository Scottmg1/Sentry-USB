package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
)

var allowedLogs = map[string]string{
	"syslog":       "/var/log/syslog",
	"kern":         "/var/log/kern.log",
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

	f, err := os.Open(logPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "Log file not found")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(logPath)+"\"")

	// Cap output to last 512KB to prevent OOM on 512MB Pi devices.
	// syslog and kern.log can grow to 50–200MB without rotation.
	const maxTail = 512 * 1024
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Cannot stat log file")
		return
	}
	if info.Size() > maxTail {
		f.Seek(-maxTail, io.SeekEnd)
		// Skip partial first line
		buf := make([]byte, 1)
		for {
			_, err := f.Read(buf)
			if err != nil || buf[0] == '\n' {
				break
			}
		}
	}
	io.Copy(w, f)
}
