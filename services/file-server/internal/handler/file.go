package handler

import "net/http"

type FileHandler struct {}

func NewFileHandler() *FileHandler {
	return &FileHandler{}
}

func (h *FileHandler) Upload(w http.ResponseWriter, r *http.Request){

}

func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request){

}

func (h *FileHandler) List(w http.ResponseWriter, r *http.Request){

}

func (h *FileHandler) DownloadMultiple(w http.ResponseWriter, r *http.Request){

}

func (h *FileHandler) Metadata(w http.ResponseWriter, r *http.Request){

}