package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (r *Router) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		started := time.Now()
		requestID := store.NewPublicID()
		w.Header().Set("X-Request-ID", requestID)
		if !strings.HasPrefix(req.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), requestIDContextKey{}, requestID)))
			r.logger.Info("http request", "method", req.Method, "path", req.URL.Path, "request_id", requestID, "duration_ms", time.Since(started).Milliseconds())
			return
		}
		recorder := &diagnosticResponseWriter{ResponseWriter: w}
		req = req.WithContext(context.WithValue(req.Context(), requestIDContextKey{}, requestID))
		next.ServeHTTP(recorder, req)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(started).Milliseconds()
		r.logger.Info("http request",
			"method", req.Method,
			"path", req.URL.Path,
			"status", status,
			"request_id", requestID,
			"duration_ms", duration,
			"response_bytes", recorder.bytes,
		)
		if duration >= 500 {
			r.logger.Info("slow http request", "method", req.Method, "path", req.URL.Path, "query", req.URL.RawQuery, "request_id", requestID, "duration_ms", duration, "response_bytes", recorder.bytes)
		}
		if strings.HasPrefix(req.URL.Path, "/api/v1/") && !strings.HasPrefix(req.URL.Path, "/api/v1/diagnostic-logs") && (status >= 400 || req.Method != http.MethodGet) {
			operation := req.Method + " " + req.URL.Path
			input := store.DiagnosticLogInput{Scope: "system", Level: "info", Component: "platform-api", Operation: operation, Message: operation + " completed", RequestID: requestID, Status: strconv.Itoa(status), DurationMS: duration, Details: map[string]any{"method": req.Method, "path": req.URL.Path, "httpStatus": status}}
			if status >= 400 {
				response := map[string]any{}
				_ = json.Unmarshal(recorder.body.Bytes(), &response)
				errorCode := auditResponseString(response, "error")
				errorMessage := auditResponseString(response, "message")
				if errorMessage == "" {
					errorMessage = errorCode
				}
				if errorMessage == "" {
					errorMessage = http.StatusText(status)
				}
				input.Level = "error"
				input.Message = operation + " failed: " + errorMessage
				input.ErrorCode = firstNonEmptyString(errorCode, http.StatusText(status))
				input.Details["errorMessage"] = errorMessage
			}
			if user, ok := requestUser(req); ok && user.TenantID != "" {
				input.Scope = "tenant"
				input.TenantID = user.TenantID
				input.Details["userId"] = user.ID
			}
			if _, err := r.store.CreateDiagnosticLog(input); err != nil {
				r.logger.Error("write diagnostic access log failed", "request_id", requestID, "error", err)
			}
		}
	})
}

type diagnosticResponseWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
	bytes  int
}

func (w *diagnosticResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *diagnosticResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.body.Len() < 64*1024 {
		remaining := 64*1024 - w.body.Len()
		if len(body) < remaining {
			remaining = len(body)
		}
		_, _ = w.body.Write(body[:remaining])
	}
	written, err := w.ResponseWriter.Write(body)
	w.bytes += written
	return written, err
}
