package auth

var routingCodexPlanDisplayTags = map[string]struct{}{
	"business":   {},
	"edu":        {},
	"enterprise": {},
	"free":       {},
	"plus":       {},
	"pro":        {},
	"team":       {},
}

func authDisplayTags(auth *Auth) []string {
	if auth == nil {
		return nil
	}
	defaultTags := make([]string, 0, 2)
	if provider := normalizeRoutingTag(auth.Provider); provider != "" && provider != "unknown" {
		defaultTags = appendUniqueTag(defaultTags, provider)
	}
	if planType := normalizeRoutingTag(metadataStringValue(auth.Metadata, "plan_type", "planType")); planType != "" {
		defaultTags = appendUniqueTag(defaultTags, planType)
	}

	customTags, _ := metadataTagList(auth.Metadata, "custom_tags")
	hiddenDefaultTags, _ := metadataTagList(auth.Metadata, "hidden_default_tags")
	explicitDisplayTags, hasExplicitDisplayTags := metadataTagList(auth.Metadata, "display_tags")
	if hasExplicitDisplayTags {
		allowed := make(map[string]struct{}, len(defaultTags)+len(customTags))
		for _, tag := range defaultTags {
			allowed[tag] = struct{}{}
		}
		for _, tag := range customTags {
			allowed[tag] = struct{}{}
		}
		out := make([]string, 0, len(explicitDisplayTags))
		providerTag := normalizeRoutingTag(auth.Provider)
		currentPlan := normalizeRoutingTag(metadataStringValue(auth.Metadata, "plan_type", "planType"))
		for _, tag := range explicitDisplayTags {
			if _, ok := allowed[tag]; ok {
				out = appendUniqueTag(out, tag)
				continue
			}
			if isStaleCodexRoutingPlanDisplayTag(providerTag, currentPlan, tag) {
				if _, ok := allowed[currentPlan]; ok {
					out = appendUniqueTag(out, currentPlan)
				}
			}
		}
		return out
	}

	hidden := make(map[string]struct{}, len(hiddenDefaultTags))
	for _, tag := range hiddenDefaultTags {
		hidden[tag] = struct{}{}
	}
	out := make([]string, 0, len(defaultTags)+len(customTags))
	for _, tag := range defaultTags {
		if _, skip := hidden[tag]; !skip {
			out = appendUniqueTag(out, tag)
		}
	}
	for _, tag := range customTags {
		if _, skip := hidden[tag]; !skip {
			out = appendUniqueTag(out, tag)
		}
	}
	return out
}

func isStaleCodexRoutingPlanDisplayTag(providerTag string, currentPlan string, tag string) bool {
	if providerTag != "codex" || currentPlan == "" || tag == currentPlan {
		return false
	}
	if _, ok := routingCodexPlanDisplayTags[currentPlan]; !ok {
		return false
	}
	_, ok := routingCodexPlanDisplayTags[tag]
	return ok
}

func authMatchesAnyTag(auth *Auth, tags []string) bool {
	if auth == nil || len(tags) == 0 {
		return false
	}
	displayTags := make(map[string]struct{})
	for _, tag := range authDisplayTags(auth) {
		normalized := normalizeRoutingTag(tag)
		if normalized != "" {
			displayTags[normalized] = struct{}{}
		}
	}
	if len(displayTags) == 0 {
		return false
	}
	for _, tag := range tags {
		if _, ok := displayTags[normalizeRoutingTag(tag)]; ok {
			return true
		}
	}
	return false
}
