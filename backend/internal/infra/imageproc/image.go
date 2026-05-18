// Package imageproc decodes image bytes, extracts intrinsic metadata
// (dimensions, format) and produces derived assets such as preview-sized
// thumbnails used by the media-derive worker.
package imageproc

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type Metadata struct {
	Width  uint32
	Height uint32
	Format string
}

func ThumbnailPNG(raw []byte, maxEdge int) ([]byte, Metadata, error) {
	if maxEdge <= 0 {
		maxEdge = 256
	}
	src, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, Metadata{}, fmt.Errorf("image: decode: %w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, Metadata{}, fmt.Errorf("image: invalid dimensions %dx%d", w, h)
	}
	tw, th := fitWithin(w, h, maxEdge)
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, Metadata{}, fmt.Errorf("image: encode thumbnail: %w", err)
	}
	return out.Bytes(), Metadata{Width: uint32(w), Height: uint32(h), Format: strings.ToUpper(format)}, nil
}

func fitWithin(w, h, maxEdge int) (int, int) {
	if w <= maxEdge && h <= maxEdge {
		return w, h
	}
	if w >= h {
		tw := maxEdge
		th := max(1, h*maxEdge/w)
		return tw, th
	}
	th := maxEdge
	tw := max(1, w*maxEdge/h)
	return tw, th
}

func ContentTypeForFormat(format string) string {
	switch strings.ToUpper(format) {
	case "JPEG", "JPG":
		return "image/jpeg"
	case "PNG":
		return "image/png"
	case "GIF":
		return "image/gif"
	case "WEBP":
		return "image/webp"
	default:
		return "image/" + strings.ToLower(format)
	}
}

