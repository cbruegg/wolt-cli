package cli

import (
	"fmt"
	"strings"

	"github.com/mekedron/wolt-cli/internal/service/observability"
	"github.com/mekedron/wolt-cli/internal/service/output"
	"github.com/spf13/cobra"
)

// newFeedCommand mirrors the wolt.com discovery home page: section-grouped
// rows of venues with marketing context (tagline, top offer, rating, ETA).
// It hits one upstream endpoint (FrontPage via Sections) — no per-venue
// enrichment by default, matching `wolt venues`' fast path.
func newFeedCommand(deps Dependencies) *cobra.Command {
	var flags globalFlags
	var lat float64
	var lon float64
	var latSet bool
	var lonSet bool
	var sectionLimit int
	var perSectionLimit int
	var query string
	var showHighlights bool
	var summary bool

	cmd := &cobra.Command{
		Use:   "feed",
		Short: "Browse the Wolt discovery feed grouped by section.",
		Long: "Browse the Wolt discovery feed grouped by section.\n\n" +
			"Mirrors wolt.com's home page: 'Popular', 'Order again', 'Fastest delivery', etc.\n" +
			"One upstream call, no per-venue enrichment. Use --section-limit / --per-section\n" +
			"to keep the table short.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := parseOutputFormat(flags.Format)
			if err != nil {
				return err
			}
			var latPtr *float64
			var lonPtr *float64
			if latSet {
				latPtr = &lat
			}
			if lonSet {
				lonPtr = &lon
			}
			locationAuth := buildAuthContextWithProfile(cmd.Context(), deps, flags)
			location, profile, err := resolveLocation(
				cmd.Context(),
				deps,
				latPtr,
				lonPtr,
				flags.Address,
				flags.Profile,
				format,
				flags.Locale,
				flags.Output,
				&locationAuth,
				cmd,
			)
			if err != nil {
				return err
			}

			sections, err := deps.Wolt.Sections(cmd.Context(), location)
			if err != nil {
				return emitUpstreamError(cmd, format, profile, flags.Locale, flags.Output, flags.Verbose, err)
			}

			var sectionLimitPtr *int
			if sectionLimit > 0 {
				sectionLimitPtr = &sectionLimit
			}
			data := observability.BuildDiscoveryFeed(sections, "", sectionLimitPtr, false)
			if needle := strings.ToLower(strings.TrimSpace(query)); needle != "" {
				filterFeedByQuery(data, needle)
			}
			if perSectionLimit > 0 {
				capFeedItemsPerSection(data, perSectionLimit)
			}

			if format == output.FormatTable {
				if summary {
					return writeTable(cmd, buildFeedSummaryTable(data), flags.Output)
				}
				effectiveHighlights := showHighlights
				if !cmd.Flags().Changed("show-highlights") {
					effectiveHighlights = anyFeedSectionHasHighlights(asSlice(data["sections"]))
				}
				return writeTable(cmd, buildFeedTable(data, effectiveHighlights), flags.Output)
			}
			env := output.BuildEnvelope(profile, flags.Locale, data, nil, nil)
			return writeMachinePayload(cmd, env, format, flags.Output)
		},
	}

	cmd.Flags().Float64Var(&lat, "lat", 0, "Latitude override for the feed location. Provide together with --lon.")
	cmd.Flags().Float64Var(&lon, "lon", 0, "Longitude override for the feed location. Provide together with --lat.")
	cmd.Flags().IntVar(&sectionLimit, "section-limit", 0, "Limit returned sections (0 = all).")
	cmd.Flags().IntVar(&perSectionLimit, "per-section", 6, "Max venues rendered per section in the table.")
	cmd.Flags().StringVar(&query, "query", "", "Filter venues by name/tagline/top-offer substring (case-insensitive).")
	cmd.Flags().BoolVar(&showHighlights, "show-highlights", true, "Append a Highlights column with venue_preview_items. Default: auto (show only when at least one row has data). Pass --show-highlights or --show-highlights=false to force.")
	cmd.Flags().BoolVar(&summary, "summary", false, "Print one line per section (title, kind, count, top items) instead of full per-section tables.")
	addGlobalFlags(cmd, &flags)
	cmd.PreRun = func(cmd *cobra.Command, _ []string) {
		latSet = cmd.Flags().Changed("lat")
		lonSet = cmd.Flags().Changed("lon")
	}
	return cmd
}

func filterFeedByQuery(data map[string]any, needle string) {
	sections := asSlice(data["sections"])
	kept := make([]any, 0, len(sections))
	for _, rawSection := range sections {
		section := asMap(rawSection)
		if section == nil {
			continue
		}
		switch asString(section["kind"]) {
		case "brands":
			brands := asSlice(section["brands"])
			matched := make([]any, 0, len(brands))
			for _, rawBrand := range brands {
				brand := asMap(rawBrand)
				if brand == nil {
					continue
				}
				haystack := strings.ToLower(asString(brand["name"]) + " " + asString(brand["slug"]))
				if strings.Contains(haystack, needle) {
					matched = append(matched, brand)
				}
			}
			if len(matched) == 0 {
				continue
			}
			section["brands"] = matched
			kept = append(kept, section)
		default:
			items := asSlice(section["items"])
			matched := make([]any, 0, len(items))
			for _, rawItem := range items {
				item := asMap(rawItem)
				if item == nil {
					continue
				}
				haystack := strings.ToLower(
					asString(item["name"]) + " " +
						asString(item["tagline"]) + " " +
						asString(item["top_offer"]) + " " +
						asString(item["slug"]),
				)
				if strings.Contains(haystack, needle) {
					matched = append(matched, item)
				}
			}
			if len(matched) == 0 {
				continue
			}
			section["items"] = matched
			kept = append(kept, section)
		}
	}
	data["sections"] = kept
}

func capFeedItemsPerSection(data map[string]any, max int) {
	if max <= 0 {
		return
	}
	for _, rawSection := range asSlice(data["sections"]) {
		section := asMap(rawSection)
		if section == nil {
			continue
		}
		switch asString(section["kind"]) {
		case "brands":
			brands := asSlice(section["brands"])
			if len(brands) > max {
				section["brands"] = brands[:max]
			}
		default:
			items := asSlice(section["items"])
			if len(items) > max {
				section["items"] = items[:max]
			}
		}
	}
}

func buildFeedTable(data map[string]any, showHighlights bool) string {
	sections := asSlice(data["sections"])
	if len(sections) == 0 {
		return output.RenderTable("Feed", []string{"Section"}, [][]string{{"(no sections)"}})
	}
	venueHeaders := []string{"Venue", "Slug", "Tagline", "Top offer", "Rating", "Delivery", "Fee"}
	if showHighlights {
		venueHeaders = append(venueHeaders, "Highlights")
	}
	chunks := make([]string, 0, len(sections))
	for _, rawSection := range sections {
		section := asMap(rawSection)
		if section == nil {
			continue
		}
		title := strings.TrimSpace(asString(section["title"]))
		if title == "" {
			title = strings.TrimSpace(asString(section["name"]))
		}
		if title == "" {
			title = "Section"
		}
		switch asString(section["kind"]) {
		case "brands":
			line := buildBrandSummaryLine(asSlice(section["brands"]))
			if line == "" {
				continue
			}
			chunks = append(chunks, output.RenderTable(title, nil, [][]string{{line}}))
		default:
			rows := buildFeedSectionRows(asSlice(section["items"]), showHighlights)
			if len(rows) == 0 {
				continue
			}
			chunks = append(chunks, output.RenderTable(title, venueHeaders, rows))
		}
	}
	if len(chunks) == 0 {
		return output.RenderTable("Feed", []string{"Section"}, [][]string{{"(no venues)"}})
	}
	return strings.Join(chunks, "\n\n")
}

// buildFeedSummaryTable renders one line per section: title, kind,
// count, and the first three names. Replaces the per-section tables
// when the user passes --summary — the "what's on the home page?"
// glance view that previously required jq.
func buildFeedSummaryTable(data map[string]any) string {
	sections := asSlice(data["sections"])
	if len(sections) == 0 {
		return output.RenderTable("Feed summary", []string{"Section"}, [][]string{{"(no sections)"}})
	}
	rows := make([][]string, 0, len(sections))
	for _, raw := range sections {
		section := asMap(raw)
		if section == nil {
			continue
		}
		title := strings.TrimSpace(asString(section["title"]))
		if title == "" {
			title = strings.TrimSpace(asString(section["name"]))
		}
		if title == "" {
			title = "Section"
		}
		kind := strings.TrimSpace(asString(section["kind"]))
		if kind == "" {
			kind = "venues"
		}
		names := summaryNamesForSection(section, kind)
		count := summaryCountForSection(section, kind)
		rows = append(rows, []string{
			title,
			kind,
			fmt.Sprintf("%d", count),
			truncateForTable(joinTopN(names, 3, " · "), 60),
		})
	}
	return output.RenderTable("Feed summary", []string{"Section", "Kind", "Count", "Top items"}, rows)
}

func summaryNamesForSection(section map[string]any, kind string) []string {
	source := "items"
	if kind == "brands" {
		source = "brands"
	}
	entries := asSlice(section[source])
	names := make([]string, 0, len(entries))
	for _, raw := range entries {
		entry := asMap(raw)
		if entry == nil {
			continue
		}
		name := strings.TrimSpace(asString(entry["name"]))
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func summaryCountForSection(section map[string]any, kind string) int {
	if kind == "brands" {
		return len(asSlice(section["brands"]))
	}
	return len(asSlice(section["items"]))
}

func joinTopN(items []string, n int, sep string) string {
	if len(items) == 0 {
		return "-"
	}
	if len(items) <= n {
		return strings.Join(items, sep)
	}
	return strings.Join(items[:n], sep) + sep + "…"
}

func buildBrandSummaryLine(brands []any) string {
	parts := make([]string, 0, len(brands))
	for _, raw := range brands {
		brand := asMap(raw)
		if brand == nil {
			continue
		}
		name := strings.TrimSpace(asString(brand["name"]))
		if name == "" {
			continue
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, " · ")
}

func buildFeedSectionRows(items []any, showHighlights bool) [][]string {
	rows := make([][]string, 0, len(items))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		rating := asString(item["rating"])
		if rating == "" {
			rating = "-"
		}
		fee := asString(asMap(item["delivery_fee"])["formatted_amount"])
		if fee == "" {
			fee = "-"
		}
		name := formatBadgePrefix(asSlice(item["badges"])) + asString(item["name"])
		row := []string{
			name,
			fallbackString(asString(item["slug"]), "-"),
			truncateForTable(asString(item["tagline"]), 32),
			truncateForTable(asString(item["top_offer"]), 26),
			rating,
			asString(item["delivery_estimate"]),
			fee,
		}
		if showHighlights {
			row = append(row, formatHighlightsCell(asSlice(item["menu_highlights"]), 32))
		}
		rows = append(rows, row)
	}
	return rows
}
