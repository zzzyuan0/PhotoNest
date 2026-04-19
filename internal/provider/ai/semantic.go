package ai

import (
	"slices"
	"strings"
)

var semanticTagSynonyms = map[string][]string{
	"people:single":                  {"single", "solo", "one person", "single person", "portrait"},
	"people:group":                   {"group", "crowd", "family", "friends", "two people", "three people", "couple"},
	"presentation:female-presenting": {"female", "woman", "girl", "women", "girls"},
	"presentation:male-presenting":   {"male", "man", "boy", "men", "boys"},
	"scene:beach":                    {"beach", "seaside", "shore", "ocean"},
	"scene:mountain":                 {"mountain", "hill", "peak"},
	"scene:city":                     {"city", "urban", "street", "downtown"},
	"scene:park":                     {"park", "garden"},
	"scene:river":                    {"river", "waterfront"},
	"scene:indoor":                   {"indoor", "inside", "room", "home"},
	"scene:car":                      {"car", "vehicle", "automobile"},
	"activity:walking":               {"walking", "strolling"},
	"activity:running":               {"running", "jogging"},
	"activity:sitting":               {"sitting", "seated"},
	"activity:inside-car":            {"inside car", "in car", "inside a car", "in a car", "driving", "riding"},
	"activity:eating":                {"eating", "dining", "meal"},
	"activity:swimming":              {"swimming", "in water"},
}

func InferSemanticSignals(values ...string) SemanticSignals {
	combined := strings.ToLower(strings.Join(values, " "))
	if combined == "" {
		return SemanticSignals{}
	}

	signals := SemanticSignals{}
	if hasAnyPhrase(combined, semanticTagSynonyms["people:group"]) {
		signals.PeopleCount = "group"
	} else if hasAnyPhrase(combined, semanticTagSynonyms["people:single"]) {
		signals.PeopleCount = "single"
	}
	if hasAnyPhrase(combined, semanticTagSynonyms["presentation:female-presenting"]) {
		signals.Presentations = append(signals.Presentations, "female")
	}
	if hasAnyPhrase(combined, semanticTagSynonyms["presentation:male-presenting"]) {
		signals.Presentations = append(signals.Presentations, "male")
	}
	for _, scene := range []struct {
		tag   string
		value string
	}{
		{tag: "scene:beach", value: "beach"},
		{tag: "scene:mountain", value: "mountain"},
		{tag: "scene:city", value: "city"},
		{tag: "scene:park", value: "park"},
		{tag: "scene:river", value: "river"},
		{tag: "scene:indoor", value: "indoor"},
		{tag: "scene:car", value: "car"},
	} {
		if hasAnyPhrase(combined, semanticTagSynonyms[scene.tag]) {
			signals.Scenes = append(signals.Scenes, scene.value)
		}
	}
	for _, activity := range []struct {
		tag   string
		value string
	}{
		{tag: "activity:walking", value: "walking"},
		{tag: "activity:running", value: "running"},
		{tag: "activity:sitting", value: "sitting"},
		{tag: "activity:inside-car", value: "inside car"},
		{tag: "activity:eating", value: "eating"},
		{tag: "activity:swimming", value: "swimming"},
	} {
		if hasAnyPhrase(combined, semanticTagSynonyms[activity.tag]) {
			signals.Activities = append(signals.Activities, activity.value)
		}
	}
	return signals
}

func NormalizeSemanticTags(signals SemanticSignals, freeText ...string) []string {
	merged := signals
	inferred := InferSemanticSignals(freeText...)
	merged.PeopleCount = firstNonEmptySemantic(merged.PeopleCount, inferred.PeopleCount)
	merged.Presentations = append(append([]string(nil), merged.Presentations...), inferred.Presentations...)
	merged.Scenes = append(append([]string(nil), merged.Scenes...), inferred.Scenes...)
	merged.Activities = append(append([]string(nil), merged.Activities...), inferred.Activities...)

	tags := make([]string, 0, 8)
	added := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := added[value]; ok {
			return
		}
		added[value] = struct{}{}
		tags = append(tags, value)
	}

	switch strings.ToLower(strings.TrimSpace(merged.PeopleCount)) {
	case "single", "solo", "one":
		add("people:single")
	case "group", "multiple", "couple":
		add("people:group")
	}
	for _, presentation := range merged.Presentations {
		switch strings.ToLower(strings.TrimSpace(presentation)) {
		case "female", "woman", "girl":
			add("presentation:female-presenting")
		case "male", "man", "boy":
			add("presentation:male-presenting")
		}
	}
	for _, scene := range merged.Scenes {
		add(normalizeSemanticCategory("scene", scene))
	}
	for _, activity := range merged.Activities {
		add(normalizeSemanticCategory("activity", activity))
	}

	slices.Sort(tags)
	return tags
}

func SearchTermsFromTags(tags []string) []string {
	terms := make([]string, 0, len(tags)*2)
	for _, tag := range tags {
		terms = append(terms, tag)
		switch tag {
		case "people:single":
			terms = append(terms, "single person", "solo portrait")
		case "people:group":
			terms = append(terms, "group photo", "family photo")
		case "presentation:female-presenting":
			terms = append(terms, "woman", "girl", "female")
		case "presentation:male-presenting":
			terms = append(terms, "man", "boy", "male")
		case "scene:beach":
			terms = append(terms, "beach", "seaside")
		case "scene:mountain":
			terms = append(terms, "mountain")
		case "scene:city":
			terms = append(terms, "city", "street")
		case "scene:park":
			terms = append(terms, "park")
		case "scene:river":
			terms = append(terms, "river")
		case "scene:indoor":
			terms = append(terms, "indoor", "inside")
		case "scene:car":
			terms = append(terms, "car", "vehicle")
		case "activity:walking":
			terms = append(terms, "walking")
		case "activity:running":
			terms = append(terms, "running")
		case "activity:sitting":
			terms = append(terms, "sitting")
		case "activity:inside-car":
			terms = append(terms, "inside car", "in car", "driving", "riding")
		case "activity:eating":
			terms = append(terms, "eating")
		case "activity:swimming":
			terms = append(terms, "swimming")
		}
	}
	return terms
}

func SemanticTags(tags []string) []string {
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if strings.Contains(tag, ":") {
			filtered = append(filtered, tag)
		}
	}
	return filtered
}

func normalizeSemanticCategory(namespace string, value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch namespace {
	case "scene":
		switch normalized {
		case "beach", "seaside", "shore", "ocean":
			return "scene:beach"
		case "mountain", "hill", "peak":
			return "scene:mountain"
		case "city", "urban", "street", "downtown":
			return "scene:city"
		case "park", "garden":
			return "scene:park"
		case "river", "waterfront":
			return "scene:river"
		case "indoor", "inside", "room", "home":
			return "scene:indoor"
		case "car", "vehicle", "automobile":
			return "scene:car"
		}
	case "activity":
		switch normalized {
		case "walking", "strolling":
			return "activity:walking"
		case "running", "jogging":
			return "activity:running"
		case "sitting", "seated":
			return "activity:sitting"
		case "inside car", "in car", "inside a car", "in a car", "driving", "riding":
			return "activity:inside-car"
		case "eating", "dining", "meal":
			return "activity:eating"
		case "swimming", "in water":
			return "activity:swimming"
		}
	}
	return ""
}

func hasAnyPhrase(value string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(value, phrase) {
			return true
		}
	}
	return false
}

func firstNonEmptySemantic(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
