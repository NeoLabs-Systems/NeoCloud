/* NeoCloud landing — download links. Release files carry their version
   (NeoCloud-Setup-v1.2.3-windows-x64.exe), so the buttons point at the
   latest release page and are swapped for the exact files once GitHub
   answers. Where the page's CSP blocks the request, the release page stays. */
(() => {
  'use strict';

  const RELEASE_API = 'https://api.github.com/repos/NeoLabs-Systems/NeoCloud/releases/latest';
  const PATTERNS = {
    'windows-x64': /^NeoCloud-Setup-.+-windows-x64\.exe$/,
    'windows-arm64': /^NeoCloud-Setup-.+-windows-arm64\.exe$/,
    macos: /^NeoCloud-.+-macos\.dmg$/,
    linux: /^NeoCloud-.+-linux-x86_64\.AppImage$/,
    android: /^neocloud-.+-android\.apk$/,
  };

  const links = document.querySelectorAll('[data-asset]');
  if (!links.length || !window.fetch) return;

  fetch(RELEASE_API, { headers: { Accept: 'application/vnd.github+json' } })
    .then((res) => (res.ok ? res.json() : null))
    .then((release) => {
      if (!release || !Array.isArray(release.assets)) return;
      links.forEach((link) => {
        const pattern = PATTERNS[link.dataset.asset];
        const asset = pattern && release.assets.find((a) => pattern.test(a.name));
        if (asset) {
          link.href = asset.browser_download_url;
          link.title = asset.name;
        }
      });
      const version = document.querySelector('[data-release-version]');
      if (version && release.tag_name) version.textContent = release.tag_name;
    })
    .catch(() => { /* offline or blocked: the release page links stay */ });
})();
