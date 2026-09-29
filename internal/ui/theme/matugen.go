package theme

import _ "embed"

//go:embed matugen.toml
var matugenTemplate string

// MatugenTemplate is the matugen template rendering a Notty user theme
// (<ConfigDir>/themes/matugen.toml). It leaves dark, success and warning to
// LoadUser's fallbacks, so it needs no matugen conditionals and works in
// both light and dark schemes.
func MatugenTemplate() string { return matugenTemplate }

// MatugenRoles are the Material 3 color roles matugen exposes, used to check
// the template only references real roles.
var MatugenRoles = []string{
	"primary", "on_primary", "primary_container", "on_primary_container",
	"primary_fixed", "primary_fixed_dim", "on_primary_fixed", "on_primary_fixed_variant",
	"secondary", "on_secondary", "secondary_container", "on_secondary_container",
	"secondary_fixed", "secondary_fixed_dim", "on_secondary_fixed", "on_secondary_fixed_variant",
	"tertiary", "on_tertiary", "tertiary_container", "on_tertiary_container",
	"tertiary_fixed", "tertiary_fixed_dim", "on_tertiary_fixed", "on_tertiary_fixed_variant",
	"error", "on_error", "error_container", "on_error_container",
	"surface", "on_surface", "surface_variant", "on_surface_variant",
	"surface_dim", "surface_bright", "surface_container_lowest", "surface_container_low",
	"surface_container", "surface_container_high", "surface_container_highest",
	"outline", "outline_variant", "background", "on_background",
	"inverse_surface", "inverse_on_surface", "inverse_primary",
	"shadow", "scrim", "surface_tint", "source_color",
}
