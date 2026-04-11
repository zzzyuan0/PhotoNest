package library

type PrivacyMode string

const (
	PrivacyModeBalanced   PrivacyMode = "balanced"
	PrivacyModeLocalOnly  PrivacyMode = "local_only"
	PrivacyModeStrictMeta PrivacyMode = "strict_metadata"
)

type Policy struct {
	Mode                 PrivacyMode
	AllowRemoteCaption   bool
	AllowRemoteOCR       bool
	AllowRemoteEmbedding bool
	AllowGPSPersistence  bool
}

func (p Policy) AllowsCapability(capability string) bool {
	switch capability {
	case "caption":
		return p.AllowRemoteCaption
	case "ocr":
		return p.AllowRemoteOCR
	case "embedding":
		return p.AllowRemoteEmbedding
	default:
		return false
	}
}
