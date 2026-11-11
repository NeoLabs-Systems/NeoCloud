# Claude Instructions

Follow the engineering standards in [GUIDELINES.md](GUIDELINES.md) for all code changes.

## Additional notes for Claude

- Flutter web builds are Neo's responsibility. Never run `flutter build web`.
- The Flutter app (`flutter_app/`) is web, Android and iOS only. The desktop
  client (Windows, macOS, Linux) is the Go lite client in `clients/lite`.
- The UI follows NeoRouter (`/Users/neo/NeoRouter/flutter_app`), the
  logo-based NeoLabs app: theme, palette, login/register, sidebar, settings
  and admin are ported 1:1 from it. Copy and rename; don't re-derive them.
  Ignore the NeoLabs-Ecosystem template.
- The logo is NeoRouter's orbital mark with the glyph turned 42°
  (`flutter_app/assets/branding/logo.svg`, `landing/assets/logo.svg`).
- The landing page (`landing/`) shares NeoRouter's `styles.css` and
  `surface.js`; NeoCloud's own visuals live in `css/neocloud.css` and
  `js/vault.js`.
- Keep [docs/api.md](docs/api.md) in step with any route change.
- Run `npm test` after server changes; tests use a temporary data directory.
