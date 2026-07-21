package metadata

// gameCategoryNames maps IGDB's game `category` enum to a human label.
var gameCategoryNames = map[int]string{
	0:  "Main game",
	1:  "DLC / Add-on",
	2:  "Expansion",
	3:  "Bundle",
	4:  "Standalone expansion",
	5:  "Mod",
	6:  "Episode",
	7:  "Season",
	8:  "Remake",
	9:  "Remaster",
	10: "Expanded game",
	11: "Port",
	12: "Fork",
	13: "Pack",
	14: "Update",
}

// ageRatingLabels maps IGDB's age-rating enum to a self-contained label. The
// enum already encodes the rating board, so no separate prefix is needed.
var ageRatingLabels = map[int]string{
	1:  "PEGI 3",
	2:  "PEGI 7",
	3:  "PEGI 12",
	4:  "PEGI 16",
	5:  "PEGI 18",
	6:  "ESRB RP",
	7:  "ESRB EC",
	8:  "ESRB E",
	9:  "ESRB E10+",
	10: "ESRB T",
	11: "ESRB M",
	12: "ESRB AO",
	13: "CERO A",
	14: "CERO B",
	15: "CERO C",
	16: "CERO D",
	17: "CERO Z",
	18: "USK 0",
	19: "USK 6",
	20: "USK 12",
	21: "USK 16",
	22: "USK 18",
}

// websiteCategories maps IGDB's website `category` enum to a link label.
var websiteCategories = map[int]string{
	1:  "Official site",
	2:  "Wiki",
	3:  "Wikipedia",
	4:  "Facebook",
	5:  "Twitter",
	6:  "Twitch",
	8:  "Instagram",
	9:  "YouTube",
	10: "iOS",
	11: "iPad",
	12: "Android",
	13: "Steam",
	14: "Reddit",
	15: "itch.io",
	16: "Epic Games",
	17: "GOG",
	18: "Discord",
}

// buildExtras assembles the display-only metadata from a parsed IGDB game.
// Slices are always non-nil so the JSON (and the frontend) see [] rather than
// null, and empty values are dropped so the UI can hide sections cleanly.
func buildExtras(p igdbGame) *GameExtras {
	e := &GameExtras{
		Storyline:          p.Storyline,
		AggregatedRating:   p.AggregatedRating,
		GameModes:          names(p.GameModes),
		PlayerPerspectives: names(p.PlayerPerspectives),
		Themes:             names(p.Themes),
		AlternativeNames:   make([]string, 0, len(p.AlternativeNames)),
		AgeRatings:         make([]string, 0, len(p.AgeRatings)),
		Websites:           make([]GameWebsite, 0, len(p.Websites)),
		ScreenshotImageIDs: make([]string, 0, len(p.Screenshots)),
		Videos:             make([]GameVideo, 0, len(p.Videos)),
		SimilarGames:       related(p.SimilarGames),
		DLCs:               related(p.DLCs),
		Expansions:         related(p.Expansions),
	}

	if p.Category != nil {
		e.Category = gameCategoryNames[*p.Category]
	}
	if p.Franchise != nil {
		e.Franchise = p.Franchise.Name
	}
	if p.Collection != nil {
		e.Collection = p.Collection.Name
	}

	for _, c := range p.InvolvedCompanies {
		if c.Company.Name == "" {
			continue
		}
		if c.Developer && e.Developer == "" {
			e.Developer = c.Company.Name
		}
		if c.Publisher && e.Publisher == "" {
			e.Publisher = c.Company.Name
		}
	}

	for _, a := range p.AlternativeNames {
		if a.Name != "" {
			e.AlternativeNames = append(e.AlternativeNames, a.Name)
		}
	}
	for _, a := range p.AgeRatings {
		if a.Rating == nil {
			continue
		}
		if label, ok := ageRatingLabels[*a.Rating]; ok {
			e.AgeRatings = append(e.AgeRatings, label)
		}
	}
	for _, w := range p.Websites {
		if w.URL == "" {
			continue
		}
		category := "Website"
		if w.Category != nil {
			if label, ok := websiteCategories[*w.Category]; ok {
				category = label
			}
		}
		e.Websites = append(e.Websites, GameWebsite{URL: w.URL, Category: category})
	}
	for _, s := range p.Screenshots {
		if s.ImageID != "" {
			e.ScreenshotImageIDs = append(e.ScreenshotImageIDs, s.ImageID)
		}
	}
	for _, v := range p.Videos {
		if v.VideoID != "" {
			name := v.Name
			if name == "" {
				name = "Trailer"
			}
			e.Videos = append(e.Videos, GameVideo{VideoID: v.VideoID, Name: name})
		}
	}
	return e
}

// names extracts non-empty Ref names into a non-nil slice.
func names(refs []Ref) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Name != "" {
			out = append(out, r.Name)
		}
	}
	return out
}

// related maps IGDB related-game rows into RelatedGame, dropping empties.
func related(rows []igdbRelated) []RelatedGame {
	out := make([]RelatedGame, 0, len(rows))
	for _, r := range rows {
		if r.Name == "" {
			continue
		}
		out = append(out, RelatedGame{ID: r.ID, Name: r.Name, CoverImageID: r.Cover.ImageID})
	}
	return out
}
