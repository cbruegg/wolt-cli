package mcpserver

import (
	"context"
	"log/slog"

	"github.com/mekedron/wolt-cli/internal/domain"
	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

// ProfileResolver resolves the default profile from persisted config.
type ProfileResolver interface {
	Find(ctx context.Context, profileName string) (domain.Profile, error)
}

// LocationResolver geocodes free-form address strings.
type LocationResolver interface {
	Get(ctx context.Context, address string) (domain.Location, error)
}

// ConfigStore reads/writes the on-disk wolt config so the MCP server can
// persist rotated tokens after an auto-refresh.
type ConfigStore interface {
	Path() string
	Load(ctx context.Context) (domain.Config, error)
	Save(ctx context.Context, cfg domain.Config) error
}

// Deps wires runtime dependencies into NewServer.
type Deps struct {
	Wolt     woltgateway.API
	Profiles ProfileResolver
	Location LocationResolver
	Config   ConfigStore
	Version  string
	Logger   *slog.Logger
}

// ToolCtx is the shared receiver every tool handler hangs off. Keeping it as a
// struct (rather than passing 5 deps through every closure) makes the tool
// files smaller and avoids accidentally varying the dependency surface.
type ToolCtx struct {
	wolt     woltgateway.API
	profiles ProfileResolver
	location LocationResolver
	config   ConfigStore
	version  string
	logger   *slog.Logger
}

func newToolCtx(deps Deps) *ToolCtx {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ToolCtx{
		wolt:     deps.Wolt,
		profiles: deps.Profiles,
		location: deps.Location,
		config:   deps.Config,
		version:  deps.Version,
		logger:   logger,
	}
}
