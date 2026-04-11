package ai

import (
	"hash/fnv"
	"math"
	"regexp"
	"slices"
	"strings"
)

var tokenSplitPattern = regexp.MustCompile(`[^a-z0-9]+`)

var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "at": {}, "for": {}, "from": {}, "img": {},
	"image": {}, "in": {}, "of": {}, "on": {}, "photo": {}, "png": {}, "jpg": {},
	"jpeg": {}, "the": {}, "to": {}, "with": {},
}

func KeywordTokens(value string) []string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return nil
	}

	parts := tokenSplitPattern.Split(normalized, -1)
	seen := map[string]struct{}{}
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < 2 {
			continue
		}
		if _, blocked := stopWords[part]; blocked {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		tokens = append(tokens, part)
	}

	return tokens
}

func HashEmbeddingText(value string, dimension int) []float32 {
	if dimension <= 0 {
		dimension = 24
	}

	vector := make([]float32, dimension)
	tokens := KeywordTokens(value)
	if len(tokens) == 0 {
		return vector
	}

	for index, token := range tokens {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(token))
		sum := hasher.Sum64()

		primary := int(sum % uint64(dimension))
		secondary := int((sum / 131) % uint64(dimension))
		weight := float32(1 + ((len(token) + index) % 4))
		if sum&1 == 1 {
			weight = -weight
		}

		vector[primary] += weight
		if secondary != primary {
			vector[secondary] += weight / 2
		}
	}

	normalizeVector(vector)
	return vector
}

func CosineSimilarity(left []float32, right []float32) float64 {
	length := min(len(left), len(right))
	if length == 0 {
		return 0
	}

	var dot, leftNorm, rightNorm float64
	for index := 0; index < length; index++ {
		l := float64(left[index])
		r := float64(right[index])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}

	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func TextPreview(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if maxRunes <= 0 || trimmed == "" {
		return trimmed
	}

	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}

	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func MergeTags(values ...string) []string {
	tags := make([]string, 0, len(values)*3)
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, token := range KeywordTokens(value) {
			if _, exists := seen[token]; exists {
				continue
			}
			seen[token] = struct{}{}
			tags = append(tags, token)
		}
	}
	slices.Sort(tags)
	return tags
}

func normalizeVector(vector []float32) {
	var norm float64
	for _, value := range vector {
		norm += float64(value * value)
	}
	if norm == 0 {
		return
	}
	scale := float32(1 / math.Sqrt(norm))
	for index := range vector {
		vector[index] *= scale
	}
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
