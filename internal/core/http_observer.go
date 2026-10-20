package core

import "net/http"

// statusLogWriter is used only on mutating HTTP methods. It deliberately
// does not wrap GET streams, which require Flusher and websocket Hijacker.
type statusLogWriter struct {
 http.ResponseWriter
 status int
}
func (w *statusLogWriter) WriteHeader(status int) {
 if w.status==0{w.status=status}
 w.ResponseWriter.WriteHeader(status)
}
func (w *statusLogWriter) Write(payload []byte)(int,error){
 if w.status==0{w.status=http.StatusOK}
 return w.ResponseWriter.Write(payload)
}
