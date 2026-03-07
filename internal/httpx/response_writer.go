package httpx

import "net/http"

type StatusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int64
}

func NewStatusCapturingResponseWriter(rw http.ResponseWriter) *StatusCapturingResponseWriter {
	return &StatusCapturingResponseWriter{ResponseWriter: rw}
}

func (w *StatusCapturingResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *StatusCapturingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *StatusCapturingResponseWriter) Write(payload []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(payload)
	w.bytes += int64(n)
	return n, err
}

func (w *StatusCapturingResponseWriter) StatusCode() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}

func (w *StatusCapturingResponseWriter) BytesWritten() int64 {
	return w.bytes
}
