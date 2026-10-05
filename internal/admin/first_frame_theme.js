// The theme of the panel's first frame (Orbit, internal/admin/appearance.go).
//
// The document loads this classic script from the panel's own origin, in
// <head> and ahead of the bundle, when the application configured a theme
// (branding.theme). The parser stops for it before there is anything to
// paint, so the first frame is already in the theme the panel keeps, and the
// policy's script-src stays 'self'.
//
// What wins, in order: the operator's own choice, made with the panel's
// toggle (orbit-theme-choice); then the configured theme, where "system"
// follows the operator's system preference. A theme kept from before this
// script existed (gf-theme) does not count as a choice: the panel wrote the
// browser's preference there on every first visit, so it cannot tell one
// from the other.
//
// It leaves the theme it applied in gf-theme, where every bundle looks for
// the last one, and marks the document so the bundle does not decide again.
(function () {
  var root = document.documentElement;
  var meta = document.querySelector('meta[name="nucleus-admin-theme"]');
  var configured = meta ? meta.getAttribute('content') : '';
  var choice = null;
  try {
    choice = window.localStorage.getItem('orbit-theme-choice');
  } catch (e) {
    choice = null;
  }
  var theme;
  var from;
  if (choice === 'dark' || choice === 'light') {
    theme = choice;
    from = 'operator';
  } else if (configured === 'dark' || configured === 'light') {
    theme = configured;
    from = 'configuration';
  } else if (configured === 'system') {
    var prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
    theme = prefersDark ? 'dark' : 'light';
    from = 'configuration';
  } else {
    return;
  }
  if (theme === 'dark') {
    root.classList.add('dark');
  } else {
    root.classList.remove('dark');
  }
  root.setAttribute('data-theme-from', from);
  try {
    window.localStorage.setItem('gf-theme', theme);
  } catch (e) {
    // Storage refused: the theme is applied all the same.
  }
})();
