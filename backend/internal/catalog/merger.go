package catalog

import (
	"sort"
	"strings"

	"modhub/internal/domain"
)

func MergeItems(rawItems []domain.RawProjectCard, enableMerge bool) []domain.ProjectCard {
	return MergeItemsWithOverrides(rawItems, enableMerge, domain.OverridesData{})
}

func MergeItemsWithOverrides(rawItems []domain.RawProjectCard, enableMerge bool, ov domain.OverridesData) []domain.ProjectCard {
	if len(rawItems) == 0 {
		return []domain.ProjectCard{}
	}

	splitPairs := map[string]struct{}{}
	for _, sp := range ov.Splits {
		for i := 0; i < len(sp); i++ {
			a := strings.ToLower(strings.TrimSpace(sp[i]))
			if a == "" {
				continue
			}
			for j := i + 1; j < len(sp); j++ {
				b := strings.ToLower(strings.TrimSpace(sp[j]))
				if b == "" || a == b {
					continue
				}
				splitPairs[a+"|"+b] = struct{}{}
				splitPairs[b+"|"+a] = struct{}{}
			}
		}
	}

	manualMergeRule := map[string]int{}
	for ruleIdx, mg := range ov.Merges {
		for _, sk := range mg {
			k := strings.ToLower(strings.TrimSpace(sk))
			if k != "" {
				manualMergeRule[k] = ruleIdx
			}
		}
	}

	var groups [][]domain.RawProjectCard
	keyToGroup := map[string]int{}
	repoToGroup := map[string]int{}
	manualRuleToGroup := map[int]int{}
	seenSK := map[string]struct{}{}

	slugsSimilar := func(a, b string) bool {
		sa := domain.NormalizeCompactTitle(a)
		sb := domain.NormalizeCompactTitle(b)
		if len(sa) < 3 || len(sb) < 3 {
			return false
		}
		return sa == sb || strings.HasPrefix(sa, sb) || strings.HasPrefix(sb, sa)
	}

	pairIsSplit := func(skA, skB string) bool {
		if len(splitPairs) == 0 || skA == "" || skB == "" {
			return false
		}
		_, blocked := splitPairs[strings.ToLower(skA)+"|"+strings.ToLower(skB)]
		return blocked
	}

	canJoinGroup := func(grp []domain.RawProjectCard, it domain.RawProjectCard, matchedByRepo bool, compLen int) bool {
		itRepo := domain.ExtractNormalizedRepo(it.Links)
		hasCorroboration := matchedByRepo
		for _, gIt := range grp {
			if pairIsSplit(gIt.SourceKey, it.SourceKey) {
				return false
			}
			if gIt.Platform == it.Platform {
				if !domain.LoadersDisjoint(gIt.Loaders, it.Loaders) {
					return false
				}
			}
			gRepo := domain.ExtractNormalizedRepo(gIt.Links)
			if itRepo != "" && gRepo != "" && itRepo != gRepo {
				return false
			}
			if !hasCorroboration {
				if (itRepo != "" && gRepo == itRepo) || AuthorsOverlap(gIt.Authors, it.Authors) || slugsSimilar(gIt.Slug, it.Slug) {
					hasCorroboration = true
				}
			}
		}
		if !hasCorroboration && compLen < 10 {
			return false
		}
		return true
	}

	for _, it := range rawItems {
		if it.SourceKey != "" {
			if _, dup := seenSK[it.SourceKey]; dup {
				continue
			}
			seenSK[it.SourceKey] = struct{}{}
		}
		if !enableMerge {
			groups = append(groups, []domain.RawProjectCard{it})
			continue
		}

		joinedIdx := -1
		lowSK := strings.ToLower(strings.TrimSpace(it.SourceKey))
		ruleIdx, hasManualRule := manualMergeRule[lowSK]
		if hasManualRule {
			if gIdx, ok := manualRuleToGroup[ruleIdx]; ok {
				blocked := false
				for _, gIt := range groups[gIdx] {
					if pairIsSplit(gIt.SourceKey, it.SourceKey) {
						blocked = true
						break
					}
				}
				if !blocked {
					joinedIdx = gIdx
				}
			}
		}

		repo := domain.ExtractNormalizedRepo(it.Links)
		if joinedIdx < 0 && repo != "" {
			rkey := it.ContentType + "::repo::" + repo
			if gIdx, ok := repoToGroup[rkey]; ok && canJoinGroup(groups[gIdx], it, true, 99) {
				joinedIdx = gIdx
			}
		}

		comp := domain.NormalizeCompactTitle(it.Title)
		compNoAcr := domain.NormalizeTitleNoAcronym(it.Title)
		if joinedIdx < 0 && comp != "" {
			mkey := it.ContentType + "::" + comp
			if gIdx, ok := keyToGroup[mkey]; ok && canJoinGroup(groups[gIdx], it, false, len(comp)) {
				joinedIdx = gIdx
			}
		}
		if joinedIdx < 0 && compNoAcr != "" && compNoAcr != comp {
			mkey2 := it.ContentType + "::" + compNoAcr
			if gIdx, ok := keyToGroup[mkey2]; ok && canJoinGroup(groups[gIdx], it, false, len(compNoAcr)) {
				joinedIdx = gIdx
			}
		}

		if joinedIdx >= 0 {
			groups[joinedIdx] = append(groups[joinedIdx], it)
			if hasManualRule {
				manualRuleToGroup[ruleIdx] = joinedIdx
			}
			if repo != "" {
				repoToGroup[it.ContentType+"::repo::"+repo] = joinedIdx
			}
			if comp != "" {
				keyToGroup[it.ContentType+"::"+comp] = joinedIdx
			}
			continue
		}

		newIdx := len(groups)
		groups = append(groups, []domain.RawProjectCard{it})
		if hasManualRule {
			manualRuleToGroup[ruleIdx] = newIdx
		}
		if repo != "" {
			repoToGroup[it.ContentType+"::repo::"+repo] = newIdx
		}
		if comp != "" {
			keyToGroup[it.ContentType+"::"+comp] = newIdx
		}
		if compNoAcr != "" && compNoAcr != comp {
			keyToGroup[it.ContentType+"::"+compNoAcr] = newIdx
		}
	}

	out := make([]domain.ProjectCard, 0, len(groups))
	for _, grp := range groups {
		sort.SliceStable(grp, func(i, j int) bool {
			if grp[i].Downloads != grp[j].Downloads {
				return grp[i].Downloads > grp[j].Downloads
			}
			return grp[i].Platform == "modrinth" && grp[j].Platform != "modrinth"
		})

		primary := grp[0]
		card := domain.ProjectCard{
			ContentType:  primary.ContentType,
			Title:        domain.CleanDisplayTitle(primary.Title),
			Slug:         primary.Slug,
			Status:       primary.Status,
			Authors:      []string{},
			Loaders:      []string{},
			GameVersions: []string{},
			Categories:   []string{},
			Platforms:    []string{},
			Sources:      []domain.ProjectSource{},
		}

		var skeys []string
		for _, it := range grp {
			card.Downloads += it.Downloads
			if card.IconURL == "" && it.IconURL != "" {
				card.IconURL = it.IconURL
			}
			if len(it.Summary) > len(card.Summary) {
				card.Summary = it.Summary
			}
			if it.DateModified > card.DateModified {
				card.DateModified = it.DateModified
			}
			if it.DateCreated != "" && (card.DateCreated == "" || it.DateCreated < card.DateCreated) {
				card.DateCreated = it.DateCreated
			}
			if card.License == "" && it.License != "" {
				card.License = it.License
			}
			if card.ClientSide == "" && it.ClientSide != "" {
				card.ClientSide = it.ClientSide
			}
			if card.ServerSide == "" && it.ServerSide != "" {
				card.ServerSide = it.ServerSide
			}
			if card.Links.SourceURL == "" {
				card.Links.SourceURL = it.Links.SourceURL
			}
			if card.Links.IssuesURL == "" {
				card.Links.IssuesURL = it.Links.IssuesURL
			}
			if card.Links.WikiURL == "" {
				card.Links.WikiURL = it.Links.WikiURL
			}
			if card.Links.DiscordURL == "" {
				card.Links.DiscordURL = it.Links.DiscordURL
			}
			for _, a := range it.Authors {
				card.Authors = domain.AppendUniqueCI(card.Authors, a)
			}
			for _, l := range it.Loaders {
				card.Loaders = domain.AppendUniqueCI(card.Loaders, strings.ToLower(l))
			}
			for _, v := range it.GameVersions {
				card.GameVersions = domain.AppendUniqueCI(card.GameVersions, v)
			}
			for _, c := range it.Categories {
				card.Categories = domain.AppendUniqueCI(card.Categories, c)
			}
			card.Platforms = domain.AppendUniqueCI(card.Platforms, it.Platform)

			authorsCopy := it.Authors
			if authorsCopy == nil {
				authorsCopy = []string{}
			}
			loadersCopy := it.Loaders
			if loadersCopy == nil {
				loadersCopy = []string{}
			}
			card.Sources = append(card.Sources, domain.ProjectSource{
				Platform:    it.Platform,
				SourceKey:   it.SourceKey,
				ID:          it.ID,
				Slug:        it.Slug,
				Title:       it.Title,
				URL:         it.URL,
				Downloads:   it.Downloads,
				Authors:     authorsCopy,
				Loaders:     loadersCopy,
				ProjectType: it.ContentType,
			})
			if it.SourceKey != "" {
				skeys = append(skeys, it.SourceKey)
			}
		}

		card.IsMerged = len(card.Sources) > 1
		if len(skeys) > 1 {
			sort.Strings(skeys)
			card.GroupID = "m::" + strings.Join(skeys, "+")
		} else {
			card.GroupID = primary.SourceKey
		}
		out = append(out, card)
	}
	return out
}
