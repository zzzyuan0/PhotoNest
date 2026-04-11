package ingestion

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"strings"

	"github.com/photonest/photonest/internal/asset"
)

const (
	thumbnailMaxDimension = 320
	previewMaxDimension   = 1280
)

type ImageMetadata struct {
	Width          int
	Height         int
	PerceptualHash string
}

type DerivativeImage struct {
	Purpose     asset.ObjectPurpose
	ContentType string
	Width       int
	Height      int
	Payload     []byte
}

func DetectMediaType(fallback string, payload []byte) string {
	if trimmed := strings.TrimSpace(fallback); trimmed != "" {
		return trimmed
	}
	if len(payload) == 0 {
		return "application/octet-stream"
	}

	return http.DetectContentType(payload)
}

func SHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func AnalyzeImage(payload []byte) (ImageMetadata, []DerivativeImage, error) {
	source, _, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		return ImageMetadata{}, nil, err
	}

	bounds := source.Bounds()
	metadata := ImageMetadata{
		Width:          bounds.Dx(),
		Height:         bounds.Dy(),
		PerceptualHash: perceptualHash(source),
	}

	thumbnail, err := encodeDerivative(source, thumbnailMaxDimension)
	if err != nil {
		return metadata, nil, err
	}
	preview, err := encodeDerivative(source, previewMaxDimension)
	if err != nil {
		return metadata, nil, err
	}

	return metadata, []DerivativeImage{
		{
			Purpose:     asset.ObjectPurposeThumbnail,
			ContentType: "image/jpeg",
			Width:       thumbnail.Bounds().Dx(),
			Height:      thumbnail.Bounds().Dy(),
			Payload:     thumbnail.Payload,
		},
		{
			Purpose:     asset.ObjectPurposePreview,
			ContentType: "image/jpeg",
			Width:       preview.Bounds().Dx(),
			Height:      preview.Bounds().Dy(),
			Payload:     preview.Payload,
		},
	}, nil
}

func HammingDistanceHex(left string, right string) (int, error) {
	leftBytes, err := hex.DecodeString(strings.TrimSpace(left))
	if err != nil {
		return 0, fmt.Errorf("decode left hash: %w", err)
	}
	rightBytes, err := hex.DecodeString(strings.TrimSpace(right))
	if err != nil {
		return 0, fmt.Errorf("decode right hash: %w", err)
	}
	if len(leftBytes) != len(rightBytes) {
		return 0, fmt.Errorf("hash length mismatch")
	}

	distance := 0
	for index := range leftBytes {
		value := leftBytes[index] ^ rightBytes[index]
		for value > 0 {
			distance += int(value & 1)
			value >>= 1
		}
	}

	return distance, nil
}

type encodedDerivative struct {
	Payload []byte
	Img     *image.RGBA
}

func (d encodedDerivative) Bounds() image.Rectangle {
	return d.Img.Bounds()
}

func encodeDerivative(source image.Image, maxDimension int) (encodedDerivative, error) {
	resized := resizeToFit(source, maxDimension)
	flattened := image.NewRGBA(resized.Bounds())
	draw.Draw(flattened, flattened.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(flattened, flattened.Bounds(), resized, resized.Bounds().Min, draw.Over)

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, flattened, &jpeg.Options{Quality: 85}); err != nil {
		return encodedDerivative{}, fmt.Errorf("encode jpeg derivative: %w", err)
	}

	return encodedDerivative{
		Payload: buffer.Bytes(),
		Img:     flattened,
	}, nil
}

func resizeToFit(source image.Image, maxDimension int) *image.RGBA {
	bounds := source.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()
	if srcWidth <= 0 || srcHeight <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	scale := 1.0
	if srcWidth > maxDimension || srcHeight > maxDimension {
		scale = math.Min(float64(maxDimension)/float64(srcWidth), float64(maxDimension)/float64(srcHeight))
	}

	dstWidth := max(int(math.Round(float64(srcWidth)*scale)), 1)
	dstHeight := max(int(math.Round(float64(srcHeight)*scale)), 1)
	destination := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))

	for y := 0; y < dstHeight; y++ {
		srcY := bounds.Min.Y + int(float64(y)*float64(srcHeight)/float64(dstHeight))
		if srcY >= bounds.Max.Y {
			srcY = bounds.Max.Y - 1
		}
		for x := 0; x < dstWidth; x++ {
			srcX := bounds.Min.X + int(float64(x)*float64(srcWidth)/float64(dstWidth))
			if srcX >= bounds.Max.X {
				srcX = bounds.Max.X - 1
			}
			destination.Set(x, y, source.At(srcX, srcY))
		}
	}

	return destination
}

func perceptualHash(source image.Image) string {
	sampled := resizeToFit(source, 8)
	total := 0
	values := make([]uint8, 0, 64)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			gray := color.GrayModel.Convert(sampled.At(x, y)).(color.Gray)
			values = append(values, gray.Y)
			total += int(gray.Y)
		}
	}

	average := uint8(total / len(values))
	var bits uint64
	for _, value := range values {
		bits <<= 1
		if value >= average {
			bits |= 1
		}
	}

	buffer := make([]byte, 8)
	binary.BigEndian.PutUint64(buffer, bits)
	return hex.EncodeToString(buffer)
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
