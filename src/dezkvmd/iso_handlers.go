package main

import (
	"net/http"
)

/*
	iso_handlers.go

	Top-level HTTP handler wrappers for the host-side ISO / disk-image library.
	These thin functions delegate to the isostore.Manager defined in
	mod/isostore/isostore.go.

	Routes are registered in register_ipkvm_apis (api.go).
*/

// handleISOList returns all stored images plus library disk-space info.
func handleISOList(w http.ResponseWriter, r *http.Request) {
	isoManager.HandleList(w, r)
}

// handleISOUpload accepts a multipart upload of one or more .iso / .img files
// under the form field "files".
func handleISOUpload(w http.ResponseWriter, r *http.Request) {
	isoManager.HandleUpload(w, r)
}

// handleISODelete removes a stored image.  DELETE with ?name=<file>.
func handleISODelete(w http.ResponseWriter, r *http.Request) {
	isoManager.HandleDelete(w, r)
}

// handleISORename renames a stored image.
// POST with JSON body {"name":"...","new_name":"..."}.
func handleISORename(w http.ResponseWriter, r *http.Request) {
	isoManager.HandleRename(w, r)
}

// handleISODownload serves a stored image as an HTTP download.  GET with ?name=<file>.
func handleISODownload(w http.ResponseWriter, r *http.Request) {
	isoManager.HandleDownload(w, r)
}
