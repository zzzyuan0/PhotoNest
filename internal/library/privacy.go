package library

type PrivacyMode string

const (
	PrivacyModeBalanced   PrivacyMode = "balanced"
	PrivacyModeLocalOnly  PrivacyMode = "local_only"
	PrivacyModeStrictMeta PrivacyMode = "strict_metadata"
)

type GPSMode string

const (
	GPSModeHidden    GPSMode = "hidden"
	GPSModeOwnerOnly GPSMode = "owner-only"
)

type TextMode string

const (
	TextModeHidden  TextMode = "hidden"
	TextModePreview TextMode = "preview"
	TextModeFull    TextMode = "full"
)

type EmbeddingMode string

const (
	EmbeddingModeDisabled EmbeddingMode = "disabled"
	EmbeddingModePrivate  EmbeddingMode = "private"
)

type Policy struct {
	Mode                 PrivacyMode
	AllowRemoteCaption   bool
	AllowRemoteOCR       bool
	AllowRemoteEmbedding bool
	AllowGPSPersistence  bool
	GPSMode              GPSMode
	OCRMode              TextMode
	CaptionMode          TextMode
	EmbeddingMode        EmbeddingMode
}

func DefaultPolicy() Policy {
	return Policy{
		Mode:                 PrivacyModeBalanced,
		AllowRemoteCaption:   true,
		AllowRemoteOCR:       true,
		AllowRemoteEmbedding: true,
		AllowGPSPersistence:  true,
		GPSMode:              GPSModeOwnerOnly,
		OCRMode:              TextModePreview,
		CaptionMode:          TextModePreview,
		EmbeddingMode:        EmbeddingModePrivate,
	}
}

func (p Policy) WithDefaults() Policy {
	defaults := DefaultPolicy()
	if p.Mode == "" {
		p.Mode = defaults.Mode
	}
	if p.GPSMode == "" {
		p.GPSMode = defaults.GPSMode
	}
	if p.OCRMode == "" {
		p.OCRMode = defaults.OCRMode
	}
	if p.CaptionMode == "" {
		p.CaptionMode = defaults.CaptionMode
	}
	if p.EmbeddingMode == "" {
		p.EmbeddingMode = defaults.EmbeddingMode
	}
	return p
}

func (p Policy) AllowsCapability(capability string) bool {
	p = p.WithDefaults()
	switch capability {
	case "caption":
		return p.CaptionMode != TextModeHidden && p.AllowRemoteCaption
	case "ocr":
		return p.OCRMode != TextModeHidden && p.AllowRemoteOCR
	case "embedding":
		return p.EmbeddingMode != EmbeddingModeDisabled && p.AllowRemoteEmbedding
	default:
		return false
	}
}

func (p Policy) ShouldRunGPS() bool {
	p = p.WithDefaults()
	return p.AllowGPSPersistence && p.GPSMode == GPSModeOwnerOnly
}

func (p Policy) ShouldRunCaption() bool {
	p = p.WithDefaults()
	return p.CaptionMode != TextModeHidden
}

func (p Policy) ShouldRunOCR() bool {
	p = p.WithDefaults()
	return p.OCRMode != TextModeHidden
}

func (p Policy) ShouldRunEmbedding() bool {
	p = p.WithDefaults()
	return p.EmbeddingMode == EmbeddingModePrivate
}

func (p Policy) CaptionVisiblePreview() bool {
	p = p.WithDefaults()
	return p.CaptionMode == TextModePreview || p.CaptionMode == TextModeFull
}

func (p Policy) OCRVisiblePreview() bool {
	p = p.WithDefaults()
	return p.OCRMode == TextModePreview || p.OCRMode == TextModeFull
}
