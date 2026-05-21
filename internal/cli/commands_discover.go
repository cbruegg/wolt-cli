package cli

import (
	"github.com/mekedron/wolt-cli/internal/service/observability"
	"github.com/mekedron/wolt-cli/internal/service/output"
	"github.com/spf13/cobra"
)

func newDiscoverCategoriesCommand(deps Dependencies) *cobra.Command {
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

	cmd := &cobra.Command{
		Use:   "categories",
		Short: "List available discovery categories.",
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
			data := observability.BuildCategoryList(sections)

			resolvedOffset, err := resolvePageOffset(limit, limitSet, offset, offsetSet, page, pageSet)
			if err != nil {
				return err
			}
			var limitPtr *int
			if limitSet {
				limitPtr = &limit
			}
			paginateFlatRows(data, "categories", limitPtr, resolvedOffset)
			if pageSet {
				data["page"] = page
			}

			if format == output.FormatTable {
				return writeTable(cmd, buildCategoryTable(data), flags.Output)
			}
			env := output.BuildEnvelope(profile, flags.Locale, data, []string{}, nil)
			return writeMachinePayload(cmd, env, format, flags.Output)
		},
	}

	cmd.Flags().Float64Var(&lat, "lat", 0, "Latitude override for location lookup. Provide together with --lon.")
	cmd.Flags().Float64Var(&lon, "lon", 0, "Longitude override for location lookup. Provide together with --lat.")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit returned categories")
	cmd.Flags().IntVar(&offset, "offset", 0, "Offset returned categories")
	cmd.Flags().IntVar(&page, "page", 0, "1-based page number (requires --limit; cannot be combined with --offset)")
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

func buildCategoryTable(data map[string]any) string {
	headers := []string{"Category", "Slug", "ID"}
	rows := [][]string{}
	for _, value := range asSlice(data["categories"]) {
		category := asMap(value)
		rows = append(rows, []string{asString(category["name"]), asString(category["slug"]), asString(category["id"])})
	}
	return output.RenderTable("Discover categories", headers, rows)
}
