package media

import (
	"fmt"
	"mime"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
)

func classifyUploadContentType(contentType string) (string, media.Type, string, error) {
	normalized, err := normalizeContentType(contentType)
	if err != nil {
		return "", "", "", err
	}
	ext, typ, ok := supportedUploadContentType(normalized)
	if !ok {
		return "", "", "", fmt.Errorf("%w: unsupported content_type %q", ErrInvalidInput, normalized)
	}
	return normalized, typ, ext, nil
}

func normalizeContentType(contentType string) (string, error) {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return "", fmt.Errorf("%w: content_type required", ErrInvalidInput)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", fmt.Errorf("%w: invalid content_type %q", ErrInvalidInput, contentType)
	}
	return strings.ToLower(mediaType), nil
}

func supportedUploadContentType(contentType string) (string, media.Type, bool) {
	switch contentType {
	case "image/png":
		return "png", media.TypeImage, true
	case "image/jpeg", "image/jpg":
		return "jpg", media.TypeImage, true
	case "image/gif":
		return "gif", media.TypeImage, true
	case "image/webp":
		return "webp", media.TypeImage, true
	case "audio/mpeg":
		return "mp3", media.TypeAudio, true
	case "audio/wav", "audio/x-wav":
		return "wav", media.TypeAudio, true
	case "audio/ogg", "audio/opus":
		return "ogg", media.TypeAudio, true
	case "audio/mp4":
		return "m4a", media.TypeAudio, true
	case "audio/aac":
		return "aac", media.TypeAudio, true
	case "audio/flac":
		return "flac", media.TypeAudio, true
	}
	return "", "", false
}
