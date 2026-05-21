package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mekedron/wolt-cli/internal/service/observability"
	"github.com/mekedron/wolt-cli/internal/service/output"
	"github.com/spf13/cobra"
)

// newTopCommand serves the "what should I eat right now?" view: flatten
// every kind=venues section from the discovery feed, dedupe by
// venue_id, and surface the first N (default 10). Upstream's section
// order already reflects curation, so we don't re-rank — the user gets
// a single, scrollable table instead of 19 per-section tables.
func newTopCommand(deps Dependencies) *cobra.Command {
	var flags globalFlags
	var lat float64
	var lon float64
	var latSet bool
	var lonSet bool
	var limit int
	var limitSet bool
	var offset int
	var offsetSet bool
	var page int
	var pageSet bool
	var query string
	var showHighlights bool
	var woltPlusOnly bool

	cmd := &cobra.Command{
		Use:   "top [N]",
		Short: "Show the top N venues from the discovery feed (default 10).",
		Long: "Flatten every venue section of the home page into a single ranked list.\n\n" +
			"Upstream's section ordering already prioritises featured/recommended\n" +
			"venues; this command just dedupes and trims to N. Pair with --query to\n" +
			"narrow by name/tagline, or --wolt-plus to keep only Wolt+ venues.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				parsed, err := strconv.Atoi(strings.TrimSpace(args[0]))
				if err != nil || parsed < 0 {
					return fmt.Errorf("invalid N: %q (expected a non-negative integer)", args[0])
				}
				limit = parsed
				limitSet = true
			}
			if limit == 0 {
				limit = 10
				limitSet = true
			}
			resolvedOffset, err := resolvePageOffset(limit, limitSet, offset, offsetSet, page, pageSet)
			if err != nil {
				return err
			}

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

			feed := observability.BuildDiscoveryFeed(sections, "", nil, woltPlusOnly)
			// Flatten enough rows up-front to cover offset + limit, then
			// trim via paginateFlatRows so --offset / --page work
			// consistently with the rest of the CLI.
			flatLimit := resolvedOffset + limit
			if flatLimit < limit {
				flatLimit = limit
			}
			rows := flattenTopVenues(asSlice(feed["sections"]), strings.ToLower(strings.TrimSpace(query)), flatLimit)
			data := map[string]any{
				"venues": rows,
			}
			paginateFlatRows(data, "venues", &limit, resolvedOffset)
			if pageSet {
				data["page"] = page
			}

			if format == output.FormatTable {
				effectiveHighlights := showHighlights
				if !cmd.Flags().Changed("show-highlights") {
					effectiveHighlights = anyVenueRowHasHighlights(asSlice(data["venues"]))
				}
				return writeTable(cmd, buildTopTable(data, effectiveHighlights), flags.Output)
			}
			env := output.BuildEnvelope(profile, flags.Locale, data, nil, nil)
			return writeMachinePayload(cmd, env, format, flags.Output)
		},
	}

	cmd.Flags().Float64Var(&lat, "lat", 0, "Latitude override. Provide together with --lon.")
	cmd.Flags().Float64Var(&lon, "lon", 0, "Longitude override. Provide together with --lat.")
	cmd.Flags().IntVar(&limit, "limit", 0, "Number of venues to return (overrides positional N; default 10).")
	cmd.Flags().IntVar(&offset, "offset", 0, "Offset returned venues (skips the first N in the flattened feed).")
	cmd.Flags().IntVar(&page, "page", 0, "1-based page number (requires --limit; cannot be combined with --offset).")
	cmd.Flags().StringVar(&query, "query", "", "Filter by venue name/tagline/top-offer substring (case-insensitive).")
	cmd.Flags().BoolVar(&showHighlights, "show-highlights", false, "Append a Highlights column with venue_preview_items. Default: auto (show only when at least one row has data).")
	cmd.Flags().BoolVar(&woltPlusOnly, "wolt-plus", false, "Only include Wolt+ venues.")
	addGlobalFlags(cmd, &flags)
	cmd.PreRun = func(cmd *cobra.Command, _ []string) {
		latSet = cmd.Flags().Changed("lat")
		lonSet = cmd.Flags().Changed("lon")
		limitSet = cmd.Flags().Changed("limit")
		offsetSet = cmd.Flags().Changed("offset")
		pageSet = cmd.Flags().Changed("page")
	}
	return cmd
}

// flattenTopVenues walks every kind=venues section of a built feed,
// dedupes by venue_id (preserving upstream order), optionally filters
// by query, and trims to limit entries.
func flattenTopVenues(sections []any, query string, limit int) []any {
	out := make([]any, 0, limit)
	seen := map[string]struct{}{}
	for _, rawSection := range sections {
		section := asMap(rawSection)
		if section == nil {
			continue
		}
		if asString(section["kind"]) != "" && asString(section["kind"]) != "venues" {
			continue
		}
		for _, rawItem := range asSlice(section["items"]) {
			item := asMap(rawItem)
			if item == nil {
				continue
			}
			id := asString(item["venue_id"])
			if id == "" {
				id = asString(item["slug"])
			}
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			if query != "" {
				haystack := strings.ToLower(
					asString(item["name"]) + " " +
						asString(item["tagline"]) + " " +
						asString(item["top_offer"]) + " " +
						asString(item["slug"]),
				)
				if !strings.Contains(haystack, query) {
					continue
				}
			}
			seen[id] = struct{}{}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func buildTopTable(data map[string]any, showHighlights bool) string {
	venues := asSlice(data["venues"])
	headers := []string{"Venue", "Slug", "Tagline", "Top offer", "Rating", "Delivery", "Fee", "Wolt+"}
	if showHighlights {
		headers = append(headers, "Highlights")
	}
	if len(venues) == 0 {
		return output.RenderTable("Top venues", headers, [][]string{{"(no venues)"}})
	}
	rows := make([][]string, 0, len(venues))
	for _, raw := range venues {
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
			boolToYesNo(asBool(item["wolt_plus"])),
		}
		if showHighlights {
			row = append(row, formatHighlightsCell(asSlice(item["menu_highlights"]), 32))
		}
		rows = append(rows, row)
	}
	title := fmt.Sprintf("Top %d venues", asInt(data["count"]))
	return output.RenderTable(title, headers, rows)
}
