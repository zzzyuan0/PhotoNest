package enrichment

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strconv"
	"strings"
	"time"

	"github.com/photonest/photonest/internal/provider/storage"
	exiflib "github.com/rwcarlsen/goexif/exif"
)

type ReverseGeocoder interface {
	Lookup(ctx context.Context, latitude float64, longitude float64) (string, error)
}

type FormattingGeocoder struct{}

func (FormattingGeocoder) Lookup(_ context.Context, latitude float64, longitude float64) (string, error) {
	return fmt.Sprintf("%.4f, %.4f", latitude, longitude), nil
}

type ExtractedMetadata struct {
	Width         int
	Height        int
	CapturedAt    *time.Time
	DeviceMake    string
	DeviceModel   string
	GPSLatitude   *float64
	GPSLongitude  *float64
	LocationLabel string
}

func ExtractMetadata(payload []byte, info storage.ObjectInfo) (ExtractedMetadata, error) {
	extracted := ExtractedMetadata{}

	if len(payload) > 0 {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(payload)); err == nil {
			extracted.Width = cfg.Width
			extracted.Height = cfg.Height
		}
		if exifData, err := exiflib.Decode(bytes.NewReader(payload)); err == nil {
			if capturedAt, err := exifData.DateTime(); err == nil {
				normalized := capturedAt.UTC()
				extracted.CapturedAt = &normalized
			}
			if makeTag, err := exifData.Get(exiflib.Make); err == nil {
				if value, err := makeTag.StringVal(); err == nil {
					extracted.DeviceMake = strings.TrimSpace(value)
				}
			}
			if modelTag, err := exifData.Get(exiflib.Model); err == nil {
				if value, err := modelTag.StringVal(); err == nil {
					extracted.DeviceModel = strings.TrimSpace(value)
				}
			}
			if latitude, longitude, err := exifData.LatLong(); err == nil {
				extracted.GPSLatitude = &latitude
				extracted.GPSLongitude = &longitude
			}
		}
	}

	overlayMetadata(&extracted, info.Metadata)
	return extracted, nil
}

func overlayMetadata(target *ExtractedMetadata, metadata map[string]string) {
	if target == nil || len(metadata) == 0 {
		return
	}

	if target.CapturedAt == nil {
		if value := firstMetadata(metadata, "captured_at", "captured-at", "photonest-captured-at"); value != "" {
			if capturedAt, err := parseMetadataTime(value); err == nil {
				target.CapturedAt = &capturedAt
			}
		}
	}
	if target.DeviceMake == "" {
		target.DeviceMake = firstMetadata(metadata, "device_make", "device-make", "photonest-device-make")
	}
	if target.DeviceModel == "" {
		target.DeviceModel = firstMetadata(metadata, "device_model", "device-model", "photonest-device-model")
	}
	if target.GPSLatitude == nil {
		if value := firstMetadata(metadata, "gps_latitude", "gps-latitude", "photonest-gps-latitude"); value != "" {
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				target.GPSLatitude = &parsed
			}
		}
	}
	if target.GPSLongitude == nil {
		if value := firstMetadata(metadata, "gps_longitude", "gps-longitude", "photonest-gps-longitude"); value != "" {
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				target.GPSLongitude = &parsed
			}
		}
	}
	if target.LocationLabel == "" {
		target.LocationLabel = firstMetadata(metadata, "location_label", "location-label", "photonest-location-label")
	}
}

func firstMetadata(metadata map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(metadata[key]); value != "" {
			return value
		}
	}
	return ""
}

func parseMetadataTime(value string) (time.Time, error) {
	candidates := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006:01:02 15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, candidate := range candidates {
		if parsed, err := time.Parse(candidate, strings.TrimSpace(value)); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported metadata time: %s", value)
}
