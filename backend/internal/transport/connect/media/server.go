// Package media adapts app/media onto the Connect transport surface.
package media

import (
	"time"

	analyticsapp "github.com/dtszwai/media-processing-service/backend/internal/app/analytics"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
)

const defaultDownloadTTL = 15 * time.Minute

type Server struct {
	svc     *mediaapp.Service
	tracker analyticsapp.Tracker
}

func NewServer(svc *mediaapp.Service, tracker analyticsapp.Tracker) *Server {
	return &Server{svc: svc, tracker: tracker}
}
