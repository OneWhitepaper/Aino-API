# OmniFlow API website branding

OmniFlow API (万流 API) is the model gateway website. Aino and MoMo Bot remain
independent desktop products. Desktop API paths, headers, credential identities
and billing policies keep their existing names and behavior.

The website uses the public site name, logo, subtitle and document URL settings.
Defaults live in the existing backend settings service and frontend branding
utility. Migration `263_omniflow_branding.sql` adopts the new name for legacy
Sub2API/Aino installations, retains custom names and custom subtitles, and does
not overwrite a logo customized after the migration. Migrations are tracked by
their full filenames; do not rename an already applied migration.

The default homepage is a native Vue interface. Administrator-configured
`home_content` continues to take precedence, followed by compact-home mode.
Authentication, dashboard destinations, registration settings, locale selection
and model-plaza access continue to use the existing application state.

The homepage request workspace and request dialog simulate responses locally.
They do not ask for an API key, send inference requests or consume quota. The
three entries are examples of protocol integration, not an account model list
or a promise of availability. Actual models and prices remain in the console.
Copied examples use `OMNIFLOW_API_KEY`, the current site's origin, and each
protocol's own path and authentication headers.

Homepage styles and local type/icon assets are isolated from console views.
Font and icon notices accompany the assets. Custom branding values continue
through the existing URL sanitization path.
