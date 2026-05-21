package mcpserver

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

func registerFavoritesTools(srv *mcp.Server, tc *ToolCtx) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mutate := &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wolt_favorites_list",
		Title:       "Favorite venues",
		Description: "List the user's favorited venues, ranked by personal preference signal at this location.",
		Annotations: readOnly,
	}, tc.handleFavoritesList)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wolt_favorites_add",
		Title:       "Favorite a venue",
		Description: "Add a venue to the user's favorites. Idempotent — adding an already-favorited venue is a no-op.",
		Annotations: mutate,
	}, tc.handleFavoritesAdd)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wolt_favorites_remove",
		Title:       "Unfavorite a venue",
		Description: "Remove a venue from the user's favorites. Idempotent.",
		Annotations: mutate,
	}, tc.handleFavoritesRemove)
}

// ---------------- wolt_favorites_list ----------------

type FavoritesListInput struct {
	LocationInput
}
type FavoritesListOutput struct {
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data"`
}

func (tc *ToolCtx) handleFavoritesList(ctx context.Context, _ *mcp.CallToolRequest, in FavoritesListInput) (*mcp.CallToolResult, FavoritesListOutput, error) {
	_, auth, err := tc.requireAuth(ctx)
	if err != nil {
		return nil, FavoritesListOutput{}, toolErr(err)
	}
	loc, _, err := tc.resolveLocation(ctx, in.LocationInput)
	if err != nil {
		return nil, FavoritesListOutput{}, toolErr(err)
	}
	payload, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.FavoriteVenues(ctx, loc, a)
	})
	if err != nil {
		return nil, FavoritesListOutput{}, toolErr(err)
	}
	count := len(asSlice(coalesceAny(payload["results"], payload["venues"], payload["items"])))
	return nil, FavoritesListOutput{
		Summary: humanCount(count, "favorite", "favorites"),
		Data:    payload,
	}, nil
}

// ---------------- wolt_favorites_add ----------------

type FavoritesAddInput struct {
	Venue string `json:"venue" jsonschema:"venue slug, 24-char id, or wolt.com URL"`
}
type FavoritesAddOutput struct {
	Summary  string         `json:"summary"`
	VenueID  string         `json:"venue_id"`
	Response map[string]any `json:"response,omitempty"`
}

func (tc *ToolCtx) handleFavoritesAdd(ctx context.Context, _ *mcp.CallToolRequest, in FavoritesAddInput) (*mcp.CallToolResult, FavoritesAddOutput, error) {
	if strings.TrimSpace(in.Venue) == "" {
		return nil, FavoritesAddOutput{}, toolErrf("venue is required")
	}
	_, auth, err := tc.requireAuth(ctx)
	if err != nil {
		return nil, FavoritesAddOutput{}, toolErr(err)
	}
	ref, err := tc.resolveVenueRef(ctx, in.Venue)
	if err != nil {
		return nil, FavoritesAddOutput{}, toolErr(err)
	}
	if ref.ID == "" {
		return nil, FavoritesAddOutput{}, toolErrf("could not resolve venue id for %q", in.Venue)
	}
	payload, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.FavoriteVenueAdd(ctx, ref.ID, a)
	})
	if err != nil {
		return nil, FavoritesAddOutput{}, toolErr(err)
	}
	return nil, FavoritesAddOutput{
		Summary:  "added venue " + ref.ID + " to favorites",
		VenueID:  ref.ID,
		Response: payload,
	}, nil
}

// ---------------- wolt_favorites_remove ----------------

type FavoritesRemoveInput struct {
	Venue string `json:"venue" jsonschema:"venue slug, 24-char id, or wolt.com URL"`
}
type FavoritesRemoveOutput struct {
	Summary  string         `json:"summary"`
	VenueID  string         `json:"venue_id"`
	Response map[string]any `json:"response,omitempty"`
}

func (tc *ToolCtx) handleFavoritesRemove(ctx context.Context, _ *mcp.CallToolRequest, in FavoritesRemoveInput) (*mcp.CallToolResult, FavoritesRemoveOutput, error) {
	if strings.TrimSpace(in.Venue) == "" {
		return nil, FavoritesRemoveOutput{}, toolErrf("venue is required")
	}
	_, auth, err := tc.requireAuth(ctx)
	if err != nil {
		return nil, FavoritesRemoveOutput{}, toolErr(err)
	}
	ref, err := tc.resolveVenueRef(ctx, in.Venue)
	if err != nil {
		return nil, FavoritesRemoveOutput{}, toolErr(err)
	}
	if ref.ID == "" {
		return nil, FavoritesRemoveOutput{}, toolErrf("could not resolve venue id for %q", in.Venue)
	}
	payload, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.FavoriteVenueRemove(ctx, ref.ID, a)
	})
	if err != nil {
		return nil, FavoritesRemoveOutput{}, toolErr(err)
	}
	return nil, FavoritesRemoveOutput{
		Summary:  "removed venue " + ref.ID + " from favorites",
		VenueID:  ref.ID,
		Response: payload,
	}, nil
}
