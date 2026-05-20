package cli

import "github.com/spf13/cobra"

func newStatusCommand(deps Dependencies) *cobra.Command {
	cmd := newAuthStatusCommand(deps)
	cmd.Use = "status"
	cmd.Short = "Check whether the saved Wolt account is logged in."
	return cmd
}

func newAccountCommand(deps Dependencies) *cobra.Command {
	cmd := newProfileShowCommand(deps)
	cmd.Use = "account"
	cmd.Short = "Show the current Wolt account."
	cmd.Long = "Show the current Wolt account.\n\nUse subcommands for orders, addresses, payments, and favorites."

	orders := newProfileOrdersListCommand(deps)
	orders.Use = "orders"
	orders.Short = "List account order history."
	cmd.AddCommand(orders)

	order := newProfileOrdersShowCommand(deps)
	order.Use = "order <purchase-id>"
	order.Short = "Show one order."
	cmd.AddCommand(order)

	cmd.AddCommand(newProfileAddressesCommand(deps))
	cmd.AddCommand(newProfilePaymentsCommand(deps))

	favorites := newProfileFavoritesCommand(deps)
	favorites.Aliases = nil
	favorites.Short = "List and manage favorite venues."
	if list, _, err := favorites.Find([]string{"list"}); err == nil && list != nil {
		list.Hidden = true
	}
	cmd.AddCommand(favorites)

	return cmd
}

func newVenuesCommand(deps Dependencies) *cobra.Command {
	cmd := newSearchVenuesCommand(deps)
	cmd.Use = "venues"
	cmd.Short = "Browse or search nearby venues."
	cmd.Long = "Browse or search nearby venues.\n\nUse --query to filter by venue name or slug."

	categories := newDiscoverCategoriesCommand(deps)
	categories.Use = "categories"
	categories.Short = "List nearby venue categories."
	cmd.AddCommand(categories)
	return cmd
}

func newSingleVenueCommand(deps Dependencies) *cobra.Command {
	cmd := newVenueShowCommand(deps)
	cmd.Use = "venue <venue>"
	cmd.Short = "Show venue details."
	cmd.Long = "Show venue details.\n\n<venue> can be a Wolt venue slug, venue id, or Wolt URL when the command can resolve it."

	menu := newVenueMenuCommand(deps)
	menu.Use = "menu <venue>"
	menu.Short = "Browse or search a venue menu."
	cmd.AddCommand(menu)

	categories := newVenueCategoriesCommand(deps)
	categories.Use = "categories <venue>"
	categories.Short = "List menu categories for a venue."
	cmd.AddCommand(categories)

	hours := newVenueHoursCommand(deps)
	hours.Use = "hours <venue>"
	hours.Short = "Show venue opening hours."
	cmd.AddCommand(hours)

	item := newItemShowCommand(deps)
	item.Use = "item <venue> <item-id>"
	item.Short = "Show one venue item."
	cmd.AddCommand(item)

	return cmd
}
